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

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

func newRiskAssessmentRepoMock(t *testing.T) (*riskAssessmentRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &riskAssessmentRepo{db: db}, mock
}

func assessmentReq() domain.CreateRiskAssessmentRequest {
	return domain.CreateRiskAssessmentRequest{
		Likelihood: 2, Impact: 2, Progress: "on track",
		ReassessmentDate: "2026-12-01", AssessedBy: "u-1", CreatedBy: "u-1",
	}
}

func expectScoreLookup(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(re("SELECT id FROM risk_score WHERE likelihood = ? AND impact = ?")).
		WithArgs(2, 2).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(5))
}

func expectAssessmentReadBack(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(re("FROM risk_assessment ra JOIN risk_score rs")).
		WithArgs(9).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "risk_id", "score_id", "progress", "date", "assessed_by", "created_at",
			"likelihood", "impact", "risk_rating", "risk_level", "color_code",
		}).AddRow(9, 42, 5, "on track", "2026-12-01", "u-1", time.Now(), 2, 2, 4, "MEDIUM", "#fa0"))
}

// TestCreateRiskAssessment_NoRiskChanges: without owner/approver/date the risk
// row is never touched and no change log is written.
func TestCreateRiskAssessment_NoRiskChanges(t *testing.T) {
	repo, mock := newRiskAssessmentRepoMock(t)
	expectScoreLookup(mock)
	mock.ExpectBegin()
	mock.ExpectExec(re("INSERT INTO risk_assessment")).WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectCommit()
	expectAssessmentReadBack(mock)

	if _, err := repo.CreateRiskAssessment(context.Background(), 42, assessmentReq()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestCreateRiskAssessment_AppliesRiskChanges: the risk update and the change
// log ride in the same transaction as the assessment, and the workflow status
// is never written.
func TestCreateRiskAssessment_AppliesRiskChanges(t *testing.T) {
	repo, mock := newRiskAssessmentRepoMock(t)
	owner, approver, date := 7, 8, "2026-12-31"
	field, oldV, newV := "owner_id", `"3"`, `"7"`
	req := assessmentReq()
	req.OwnerID, req.ManagementApproverID, req.ImplementationDate = &owner, &approver, &date
	req.ChangeLog = []domain.ChangeLogEntry{{Action: "UPDATE", FieldChanged: &field, OldValue: &oldV, NewValue: &newV}}

	expectScoreLookup(mock)
	mock.ExpectBegin()
	mock.ExpectExec(re("INSERT INTO risk_assessment")).WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectExec(re("UPDATE risk SET owner_id = ?, management_approver_id = ?, implementation_date = ?, updated_by = ?, updated_at = NOW() WHERE id = ?")).
		WithArgs(7, 8, "2026-12-31", "u-1", 42).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(re("INSERT INTO risk_change_log")).
		WithArgs(42, "u-1", "UPDATE", &field, &oldV, &newV, (*string)(nil)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	expectAssessmentReadBack(mock)

	if _, err := repo.CreateRiskAssessment(context.Background(), 42, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestCreateRiskAssessment_RollsBackWhenRiskUpdateFails: a failed risk update
// must not leave the assessment behind.
func TestCreateRiskAssessment_RollsBackWhenRiskUpdateFails(t *testing.T) {
	repo, mock := newRiskAssessmentRepoMock(t)
	owner := 7
	req := assessmentReq()
	req.OwnerID = &owner

	expectScoreLookup(mock)
	mock.ExpectBegin()
	mock.ExpectExec(re("INSERT INTO risk_assessment")).WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectExec(re("UPDATE risk SET")).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	if _, err := repo.CreateRiskAssessment(context.Background(), 42, req); err == nil {
		t.Fatal("expected an error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
