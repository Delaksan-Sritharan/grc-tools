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
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

var changeLogCols = []string{
	"id", "risk_id", "created_by", "action", "field_changed", "old_value", "new_value", "details", "created_at",
	"risk_code", "risk_title",
}

func newChangeLogRepoMock(t *testing.T) (*riskChangeLogRepo, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &riskChangeLogRepo{db: db}, mock
}

func intp(v int) *int { return &v }

// An unscoped, unfiltered search has no restriction beyond the join.
func TestSearchRiskChangeLogUnscoped(t *testing.T) {
	r, mock := newChangeLogRepoMock(t)
	mock.ExpectQuery(re("SELECT COUNT(*) FROM risk_change_log c JOIN risk r ON r.id = c.risk_id WHERE 1=1")).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
	mock.ExpectQuery(re("ORDER BY c.created_at DESC, c.id DESC LIMIT ? OFFSET ?")).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows(changeLogCols).
			AddRow(5, 7, "u-1", "APPROVE", nil, nil, nil, `{"role":"Compliance"}`, time.Now(), "2026-ASG-Q1-001", "Title"))

	items, total, err := r.SearchRiskChangeLog(context.Background(), domain.SearchRiskChangeLogRequest{Limit: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].RiskCode != "2026-ASG-Q1-001" || items[0].Action != "APPROVE" {
		t.Errorf("got total=%d items=%+v", total, items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// Team scope, filters and the escalation-lead widening all reach the query, in
// the order the count and the page share their arguments.
func TestSearchRiskChangeLogScopedAndFiltered(t *testing.T) {
	r, mock := newChangeLogRepoMock(t)
	req := domain.SearchRiskChangeLogRequest{
		Limit: 10, Offset: 20,
		From: "2026-01-01", To: "2026-01-31",
		Actions:                []string{"UPDATE", "ESCALATE"},
		RiskQuery:              "50%_off",
		EscalationLeadUUID:     "lead-uuid",
		ScopeSourceRegisterIDs: []int{3},
		ScopeAssignmentTeamIDs: []int{3, 4},
	}
	args := []driver.Value{
		"UPDATE", "ESCALATE", "2026-01-01 00:00:00", "2026-02-01 00:00:00",
		`%50\%\_off%`, `%50\%\_off%`,
		3, 3, 4, "lead-uuid", "lead-uuid",
	}
	mock.ExpectQuery(re("SELECT COUNT(*)")).WithArgs(args...).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
	mock.ExpectQuery(re("c.action IN (?,?)")).WithArgs(append(args, 10, 20)...).
		WillReturnRows(sqlmock.NewRows(changeLogCols))

	items, total, err := r.SearchRiskChangeLog(context.Background(), req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 0 || items == nil || len(items) != 0 {
		t.Errorf("want empty non-nil page, got total=%d items=%v", total, items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// An Action Owner sees only changes on risks whose plan they own.
func TestRiskVisibilityClauseActionOwner(t *testing.T) {
	clause, args := riskVisibilityClause("r", intp(9), "", nil, nil)
	if !strings.Contains(clause, "ap.action_owner_id = ?") || len(args) != 1 || args[0] != 9 {
		t.Errorf("clause=%q args=%v", clause, args)
	}
	if c, a := riskVisibilityClause("r", nil, "lead", nil, nil); c != "" || a != nil {
		t.Errorf("no scope must be unrestricted even with a lead: %q %v", c, a)
	}
}
