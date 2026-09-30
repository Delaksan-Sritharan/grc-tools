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

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

// RiskReminderRepository defines persistence for risk_reminder — the
// due-date reminder job's de-dup log. There is deliberately no read or list
// operation: nothing displays these rows, they exist only to answer "has this
// reminder already been sent?" atomically.
type RiskReminderRepository interface {
	// ClaimRiskReminder is the reminder sweep's atomic de-dup claim — see
	// risk_schema.sql's risk_reminder comment.
	ClaimRiskReminder(ctx context.Context, req domain.ClaimRiskReminderRequest) (id int64, claimed bool, err error)
	// ReleaseRiskReminderClaim deletes a claim row so its reminder becomes
	// sendable again — used only when the email failed after the claim
	// succeeded.
	ReleaseRiskReminderClaim(ctx context.Context, id int64) error
}

type riskReminderRepo struct{ db *sql.DB }

// NewRiskReminderRepository constructs a RiskReminderRepository.
func NewRiskReminderRepository(db *sql.DB) RiskReminderRepository {
	return &riskReminderRepo{db: db}
}

// ClaimRiskReminder atomically inserts a risk_reminder row for one risk, tier
// and due date, iff no such row exists yet (uq_risk_reminder). The insert IS
// the claim: the caller that wins it is the sole owner of sending that
// reminder, which is what keeps the several backend replicas — each running
// its own daily sweep — from all emailing the same risk. Losing the race is
// reported as claimed=false, not an error.
//
// The claim id comes straight from the insert, with no read-back: the insert
// has already committed, so any failure after it would leave a claim nobody
// knows they hold, and the sweep fails closed on an error — the reminder
// would never be sent.
func (r *riskReminderRepo) ClaimRiskReminder(ctx context.Context, req domain.ClaimRiskReminderRequest) (int64, bool, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO risk_reminder (risk_id, reminder_type, due_date_snapshot, created_by)
		 VALUES (?, ?, ?, 'system')`,
		req.RiskID, req.ReminderType, req.DueDateSnapshot,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return 0, false, nil
		}
		if isFKViolation(err) {
			return 0, false, &apierror.NotFoundError{Msg: fmt.Sprintf("risk %d not found", req.RiskID)}
		}
		return 0, false, fmt.Errorf("risk_reminder.Claim: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("risk_reminder.Claim last insert id: %w", err)
	}
	return id, true, nil
}

// ReleaseRiskReminderClaim deletes a claim row so the reminder is sendable
// again on a later run that same day. Unlike audit's equivalent this needs no
// type filter: every row in this table is a reminder claim, so there is no
// other kind of row it could be pointed at.
func (r *riskReminderRepo) ReleaseRiskReminderClaim(ctx context.Context, id int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM risk_reminder WHERE id = ?`, id); err != nil {
		return fmt.Errorf("risk_reminder.ReleaseClaim: %w", err)
	}
	return nil
}
