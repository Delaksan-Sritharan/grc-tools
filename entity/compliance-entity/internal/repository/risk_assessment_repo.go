// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

// RiskAssessmentRepository defines persistence for risk_assessment.
type RiskAssessmentRepository interface {
	CreateRiskAssessment(ctx context.Context, riskID int, req domain.CreateRiskAssessmentRequest) (*domain.RiskAssessment, error)
	ListRiskAssessments(ctx context.Context, riskID int) (*domain.ListRiskAssessmentsResponse, error)
}

type riskAssessmentRepo struct{ db *sql.DB }

// NewRiskAssessmentRepository constructs a RiskAssessmentRepository.
func NewRiskAssessmentRepository(db *sql.DB) RiskAssessmentRepository {
	return &riskAssessmentRepo{db: db}
}

// assessmentColumns is the shared projection for reads. risk_score is joined so
// the response carries the resolved residual score, not just its surrogate key.
const assessmentColumns = `ra.id, ra.risk_id, ra.score_id, ra.progress,
	DATE_FORMAT(ra.reassessment_date,'%Y-%m-%d'), ra.assessed_by, ra.created_at,
	rs.likelihood, rs.impact, rs.risk_rating, rs.risk_level, rs.color_code`

// scanAssessment reads one row of assessmentColumns.
func scanAssessment(s interface{ Scan(...any) error }) (*domain.RiskAssessment, error) {
	var a domain.RiskAssessment
	if err := s.Scan(
		&a.ID, &a.RiskID, &a.ScoreID, &a.Progress, &a.ReassessmentDate, &a.AssessedBy, &a.CreatedOn,
		&a.ResidualLikelihood, &a.ResidualImpact, &a.ResidualRating, &a.ResidualLevel, &a.ResidualColorCode,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateRiskAssessment resolves the likelihood/impact pair to a risk_score row
// and inserts the assessment. Resolving here rather than making the caller send
// a score_id keeps the score matrix an implementation detail of this service.
func (r *riskAssessmentRepo) CreateRiskAssessment(ctx context.Context, riskID int, req domain.CreateRiskAssessmentRequest) (*domain.RiskAssessment, error) {
	var scoreID int
	var level string
	err := r.db.QueryRowContext(ctx,
		"SELECT id, risk_level FROM risk_score WHERE likelihood = ? AND impact = ?",
		req.Likelihood, req.Impact).Scan(&scoreID, &level)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &apierror.ValidationError{
			Msg: fmt.Sprintf("no risk score for likelihood %d and impact %d", req.Likelihood, req.Impact),
		}
	}
	if err != nil {
		return nil, fmt.Errorf("risk_assessment.Create resolve score: %w", err)
	}

	// A reassessment that makes the risk HIGH needs a Management Approver: the
	// one it already has, or the one this request names. A risk created
	// LOW/MEDIUM has none, so it must be supplied here.
	if level == "HIGH" && (req.ManagementApproverID == nil || *req.ManagementApproverID <= 0) {
		var current sql.NullInt64
		err = r.db.QueryRowContext(ctx,
			"SELECT management_approver_id FROM risk WHERE id = ?", riskID).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("risk_assessment.Create read approver: %w", err)
		}
		// A missing risk falls through to the insert's not-found handling.
		if err == nil && !current.Valid {
			return nil, &apierror.ValidationError{
				Msg: "A Management Approver is required for a HIGH risk. Ask the Risk Assigner to set it.",
			}
		}
	}

	// One transaction: the assessment, the optional risk-field changes and
	// their change-log rows land together or not at all. Over HTTP the caller
	// cannot retry half of a reassessment.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("risk_assessment.Create begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx,
		`INSERT INTO risk_assessment (risk_id, score_id, progress, reassessment_date, assessed_by, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		riskID, scoreID, req.Progress,
		req.ReassessmentDate, req.AssessedBy, req.CreatedBy)
	if err != nil {
		if isFKViolation(err) {
			return nil, &apierror.NotFoundError{Msg: fmt.Sprintf("risk %d not found", riskID)}
		}
		return nil, fmt.Errorf("risk_assessment.Create: %w", err)
	}
	id, _ := res.LastInsertId()

	sets := []string{}
	args := []any{}
	if req.OwnerID != nil {
		sets = append(sets, "owner_id = ?")
		args = append(args, *req.OwnerID)
	}
	if req.ManagementApproverID != nil {
		sets = append(sets, "management_approver_id = ?")
		args = append(args, *req.ManagementApproverID)
	}
	if req.ImplementationDate != nil {
		sets = append(sets, "implementation_date = ?")
		args = append(args, *req.ImplementationDate)
	}
	if req.AssignerID != nil {
		sets = append(sets, "assigner_id = ?")
		args = append(args, *req.AssignerID)
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_by = ?", "updated_at = NOW()")
		args = append(args, req.CreatedBy, riskID)
		if _, err = tx.ExecContext(ctx,
			"UPDATE risk SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			if isFKViolation(err) {
				return nil, &apierror.ValidationError{Msg: "assignerId, ownerId or managementApproverId does not match a user"}
			}
			return nil, fmt.Errorf("risk_assessment.Create update risk: %w", err)
		}
	}
	// The risk's own plan is its first STANDARD one — the same pick as
	// risk.Update, so the owner written here is the one the risk shows.
	if req.ActionOwnerID != nil {
		if _, err = tx.ExecContext(ctx, `
			UPDATE risk_action_plan SET action_owner_id = ?, updated_by = ?, updated_at = NOW()
			WHERE risk_id = ? AND plan_type = 'STANDARD'
			ORDER BY id LIMIT 1`,
			*req.ActionOwnerID, req.CreatedBy, riskID); err != nil {
			if isFKViolation(err) {
				return nil, &apierror.ValidationError{Msg: "actionOwnerId does not match a user"}
			}
			return nil, fmt.Errorf("risk_assessment.Create update action plan: %w", err)
		}
	}

	for _, e := range req.ChangeLog {
		action := e.Action
		if action == "" {
			action = "UPDATE"
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO risk_change_log (risk_id, created_by, action, field_changed, old_value, new_value, details)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			riskID, req.CreatedBy, action, e.FieldChanged, e.OldValue, e.NewValue, e.Details); err != nil {
			return nil, fmt.Errorf("risk_assessment.Create change log: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("risk_assessment.Create commit: %w", err)
	}
	return r.getByID(ctx, int(id))
}

func (r *riskAssessmentRepo) getByID(ctx context.Context, id int) (*domain.RiskAssessment, error) {
	a, err := scanAssessment(r.db.QueryRowContext(ctx,
		`SELECT `+assessmentColumns+`
		 FROM risk_assessment ra JOIN risk_score rs ON rs.id = ra.score_id
		 WHERE ra.id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("risk_assessment.GetByID(%d): %w", id, err)
	}
	return a, nil
}

func (r *riskAssessmentRepo) ListRiskAssessments(ctx context.Context, riskID int) (*domain.ListRiskAssessmentsResponse, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+assessmentColumns+`
		 FROM risk_assessment ra JOIN risk_score rs ON rs.id = ra.score_id
		 WHERE ra.risk_id = ? ORDER BY ra.created_at DESC`,
		riskID)
	if err != nil {
		return nil, fmt.Errorf("risk_assessment.List: %w", err)
	}
	defer rows.Close()

	var assessments []domain.RiskAssessment
	for rows.Next() {
		a, err := scanAssessment(rows)
		if err != nil {
			return nil, fmt.Errorf("risk_assessment.List scan: %w", err)
		}
		assessments = append(assessments, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("risk_assessment.List rows: %w", err)
	}
	return &domain.ListRiskAssessmentsResponse{Assessments: assessments}, nil
}
