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
	"fmt"
	"strings"
	"time"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

// RiskChangeLogRepository defines persistence for risk_change_log (append-only).
type RiskChangeLogRepository interface {
	CreateRiskChangeLog(ctx context.Context, riskID int, req domain.CreateRiskChangeLogRequest) (*domain.RiskChangeLog, error)
	ListRiskChangeLog(ctx context.Context, riskID int, limit, offset int) ([]domain.RiskChangeLog, int, error)
	SearchRiskChangeLog(ctx context.Context, req domain.SearchRiskChangeLogRequest) ([]domain.RiskChangeLogItem, int, error)
}

type riskChangeLogRepo struct{ db *sql.DB }

// NewRiskChangeLogRepository constructs a RiskChangeLogRepository.
func NewRiskChangeLogRepository(db *sql.DB) RiskChangeLogRepository {
	return &riskChangeLogRepo{db: db}
}

func (r *riskChangeLogRepo) CreateRiskChangeLog(ctx context.Context, riskID int, req domain.CreateRiskChangeLogRequest) (*domain.RiskChangeLog, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO risk_change_log (risk_id, created_by, action, field_changed, old_value, new_value, details)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		riskID,
		req.CreatedBy,
		req.Action,
		nullableString(req.FieldChanged),
		nullableString(req.OldValue),
		nullableString(req.NewValue),
		nullableString(req.Details),
	)
	if err != nil {
		if isFKViolation(err) {
			return nil, &apierror.NotFoundError{Msg: fmt.Sprintf("risk %d not found", riskID)}
		}
		return nil, fmt.Errorf("risk_change_log.Create: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.getByID(ctx, id)
}

func (r *riskChangeLogRepo) getByID(ctx context.Context, id int64) (*domain.RiskChangeLog, error) {
	return scanRiskChangeLog(r.db.QueryRowContext(ctx,
		`SELECT id, risk_id, created_by, action, field_changed, old_value, new_value, details, created_at
		 FROM risk_change_log WHERE id = ?`, id))
}

func (r *riskChangeLogRepo) ListRiskChangeLog(ctx context.Context, riskID int, limit, offset int) ([]domain.RiskChangeLog, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM risk_change_log WHERE risk_id = ?", riskID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("risk_change_log.ListCount: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT id, risk_id, created_by, action, field_changed, old_value, new_value, details, created_at
		 FROM risk_change_log WHERE risk_id = ?
		 ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		riskID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("risk_change_log.List: %w", err)
	}
	defer rows.Close()

	var entries []domain.RiskChangeLog
	for rows.Next() {
		e, err := scanRiskChangeLog(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("risk_change_log.List scan: %w", err)
		}
		entries = append(entries, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("risk_change_log.List rows: %w", err)
	}
	return entries, total, nil
}

// SearchRiskChangeLog lists changes across risks, newest first, limited to the
// risks the request's scope allows. The scope clause mirrors SearchRisks (see
// riskVisibilityClause) so the audit trail never shows a risk the register hides.
func (r *riskChangeLogRepo) SearchRiskChangeLog(ctx context.Context, req domain.SearchRiskChangeLogRequest) ([]domain.RiskChangeLogItem, int, error) {
	where := " WHERE 1=1"
	var args []any
	if len(req.Actions) > 0 {
		where += " AND c.action IN (" + placeholders(len(req.Actions)) + ")"
		for _, a := range req.Actions {
			args = append(args, a)
		}
	}
	// created_at is a datetime, so the bounds are whole days.
	if req.From != "" {
		where += " AND c.created_at >= ?"
		args = append(args, req.From+" 00:00:00")
	}
	if req.To != "" {
		// Half-open, so the whole of the "to" day is in whatever the column's
		// sub-second precision. The service has already checked the format.
		to, _ := time.Parse("2006-01-02", req.To)
		where += " AND c.created_at < ?"
		args = append(args, to.AddDate(0, 0, 1).Format("2006-01-02")+" 00:00:00")
	}
	if q := strings.TrimSpace(req.RiskQuery); q != "" {
		like := "%" + likeEscape(q) + "%"
		where += " AND (r.risk_code LIKE ? OR r.risk_title LIKE ?)"
		args = append(args, like, like)
	}
	visClause, visArgs := riskVisibilityClause("r", req.ActionOwnerID, req.EscalationLeadUUID,
		req.ScopeSourceRegisterIDs, req.ScopeAssignmentTeamIDs)
	where += visClause
	args = append(args, visArgs...)

	const from = " FROM risk_change_log c JOIN risk r ON r.id = c.risk_id"
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*)"+from+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("risk_change_log.SearchCount: %w", err)
	}

	pageArgs := append(append([]any{}, args...), req.Limit, req.Offset)
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.id, c.risk_id, c.created_by, c.action, c.field_changed, c.old_value, c.new_value, c.details, c.created_at,
		        r.risk_code, r.risk_title`+from+where+
			` ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("risk_change_log.Search: %w", err)
	}
	defer rows.Close()

	items := []domain.RiskChangeLogItem{}
	for rows.Next() {
		var it domain.RiskChangeLogItem
		var fieldChanged, oldValue, newValue, details sql.NullString
		if err := rows.Scan(&it.ID, &it.RiskID, &it.CreatedBy, &it.Action,
			&fieldChanged, &oldValue, &newValue, &details, &it.CreatedOn,
			&it.RiskCode, &it.RiskTitle); err != nil {
			return nil, 0, fmt.Errorf("risk_change_log.Search scan: %w", err)
		}
		if fieldChanged.Valid {
			it.FieldChanged = &fieldChanged.String
		}
		if oldValue.Valid {
			it.OldValue = &oldValue.String
		}
		if newValue.Valid {
			it.NewValue = &newValue.String
		}
		if details.Valid {
			it.Details = &details.String
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("risk_change_log.Search rows: %w", err)
	}
	return items, total, nil
}

func scanRiskChangeLog(s scanner) (*domain.RiskChangeLog, error) {
	var e domain.RiskChangeLog
	var fieldChanged, oldValue, newValue, details sql.NullString
	err := s.Scan(
		&e.ID, &e.RiskID, &e.CreatedBy, &e.Action,
		&fieldChanged, &oldValue, &newValue, &details, &e.CreatedOn,
	)
	if err != nil {
		return nil, err
	}
	if fieldChanged.Valid {
		e.FieldChanged = &fieldChanged.String
	}
	if oldValue.Valid {
		e.OldValue = &oldValue.String
	}
	if newValue.Valid {
		e.NewValue = &newValue.String
	}
	if details.Valid {
		e.Details = &details.String
	}
	return &e, nil
}
