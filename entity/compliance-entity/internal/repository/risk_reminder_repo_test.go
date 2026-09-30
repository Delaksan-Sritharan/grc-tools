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
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

func newRiskReminderRepoMock(t *testing.T) (*riskReminderRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &riskReminderRepo{db: db}, mock
}

func claimReq() domain.ClaimRiskReminderRequest {
	return domain.ClaimRiskReminderRequest{
		RiskID:          42,
		ReminderType:    "DUE_IN_5_DAYS",
		DueDateSnapshot: "2026-10-06",
	}
}

// TestClaimRiskReminder_Wins covers the sweep that gets there first: the
// insert succeeds and the row it wrote comes back, so this caller owns
// sending the email.
func TestClaimRiskReminder_Wins(t *testing.T) {
	repo, mock := newRiskReminderRepoMock(t)

	mock.ExpectExec(re("INSERT INTO risk_reminder")).
		WithArgs(42, "DUE_IN_5_DAYS", "2026-10-06").
		WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectQuery(re("SELECT id, risk_id, reminder_type")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "risk_id", "reminder_type", "due_date_snapshot", "created_by", "created_at",
		}).AddRow(7, 42, "DUE_IN_5_DAYS", "2026-10-06", "system", time.Now()))

	rem, claimed, err := repo.ClaimRiskReminder(context.Background(), claimReq())
	if err != nil {
		t.Fatalf("ClaimRiskReminder: %v", err)
	}
	if !claimed {
		t.Fatal("claimed = false, want true")
	}
	if rem.ID != 7 || rem.DueDateSnapshot != "2026-10-06" {
		t.Fatalf("got %+v, want id 7 on 2026-10-06", rem)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestClaimRiskReminder_AlreadyClaimed is the case the unique key exists for:
// another replica's sweep claimed this reminder first. That must read as
// claimed=false, NOT as an error — a losing race is the expected outcome on
// every multi-replica run, and treating it as a failure would make the sweep
// log noise every morning.
func TestClaimRiskReminder_AlreadyClaimed(t *testing.T) {
	repo, mock := newRiskReminderRepoMock(t)

	mock.ExpectExec(re("INSERT INTO risk_reminder")).
		WithArgs(42, "DUE_IN_5_DAYS", "2026-10-06").
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"})

	rem, claimed, err := repo.ClaimRiskReminder(context.Background(), claimReq())
	if err != nil {
		t.Fatalf("duplicate key must not be an error, got %v", err)
	}
	if claimed {
		t.Fatal("claimed = true, want false")
	}
	if rem != nil {
		t.Fatalf("reminder = %+v, want nil", rem)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestClaimRiskReminder_UnknownRisk covers a claim for a risk that no longer
// exists (cancelled and hard-deleted between the sweep's query and its
// claim): the FK rejects it, and that is a 404, not a 500.
func TestClaimRiskReminder_UnknownRisk(t *testing.T) {
	repo, mock := newRiskReminderRepoMock(t)

	mock.ExpectExec(re("INSERT INTO risk_reminder")).
		WillReturnError(&mysql.MySQLError{Number: 1452, Message: "Cannot add or update a child row"})

	_, claimed, err := repo.ClaimRiskReminder(context.Background(), claimReq())
	if claimed {
		t.Fatal("claimed = true, want false")
	}
	var notFound *apierror.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("err = %v, want NotFoundError", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestReleaseRiskReminderClaim covers the failed-send path: the row is
// deleted so a later run the same day sends the reminder instead.
func TestReleaseRiskReminderClaim(t *testing.T) {
	repo, mock := newRiskReminderRepoMock(t)

	mock.ExpectExec(re("DELETE FROM risk_reminder WHERE id = ?")).
		WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.ReleaseRiskReminderClaim(context.Background(), 7); err != nil {
		t.Fatalf("ReleaseRiskReminderClaim: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
