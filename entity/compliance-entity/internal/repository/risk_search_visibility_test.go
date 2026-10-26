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
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package repository

import (
	"context"
	"database/sql/driver"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

// SearchRisks takes its Action Owner, team and escalation-lead restriction from
// riskVisibilityClause, the same rule the audit trail uses, so the clause and
// its args reach the query exactly as the shared helper builds them.
func TestSearchRisksUsesSharedVisibilityClause(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	r := &riskRepo{db: db}

	req := domain.SearchRisksRequest{
		ActionOwnerID:          intp(9),
		EscalationLeadUUID:     "lead-uuid",
		ScopeSourceRegisterIDs: []int{1},
		ScopeAssignmentTeamIDs: []int{2},
	}
	clause, args := riskVisibilityClause("r", req.ActionOwnerID, req.EscalationLeadUUID,
		req.ScopeSourceRegisterIDs, req.ScopeAssignmentTeamIDs)

	mock.ExpectQuery(re(clause)).
		WithArgs(driverArgs(args)...).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
	mock.ExpectQuery(re(clause)).
		WithArgs(driverArgs(append(append([]any{}, args...), 0, 0))...).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, _, err := r.SearchRisks(context.Background(), req); err != nil {
		t.Fatalf("SearchRisks: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func driverArgs(in []any) []driver.Value {
	out := make([]driver.Value, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}
