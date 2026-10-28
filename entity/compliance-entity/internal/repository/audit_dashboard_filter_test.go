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
	"reflect"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
)

func TestApplyAuditFilter(t *testing.T) {
	t.Run("no ids leaves the clause and args untouched", func(t *testing.T) {
		where, args := applyAuditFilter("WHERE a.status = 'ACTIVE' AND c.owner_id = ?", []any{9}, nil)
		if where != "WHERE a.status = 'ACTIVE' AND c.owner_id = ?" || !reflect.DeepEqual(args, []any{9}) {
			t.Errorf("got %q %v, want unchanged", where, args)
		}
	})

	t.Run("ids append after the scope args, in placeholder order", func(t *testing.T) {
		where, args := applyAuditFilter("WHERE a.status = 'ACTIVE' AND c.owner_id = ?", []any{9}, []int{4, 7})
		wantWhere := "WHERE a.status = 'ACTIVE' AND c.owner_id = ? AND c.audit_id IN (?,?)"
		if where != wantWhere || !reflect.DeepEqual(args, []any{9, 4, 7}) {
			t.Errorf("got %q %v, want %q [9 4 7]", where, args, wantWhere)
		}
	})

	t.Run("does not alias the caller's args", func(t *testing.T) {
		backing := make([]any, 1, 8) // spare capacity: a plain append would write into it
		backing[0] = 9
		_, args := applyAuditFilter("WHERE 1=1", backing, []int{4})
		args[0] = "changed"
		_ = backing[:2] // the spare slot must still be untouched
		if backing[0] != 9 || backing[:2][1] != nil {
			t.Errorf("caller's slice was modified: %v", backing[:2])
		}
	})
}

// The Completed tab lists COMPLETE controls only, under the work-queue scope
// plus the audit filter, and reports the matching total.
func TestGetWorkQueuePage_CompletedTab(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	repo := &dashboardRepo{db: db}

	mock.ExpectQuery(re("SELECT COUNT(*) FROM audit_control c")).
		WithArgs(9, 4).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
	mock.ExpectQuery(re("c.status = 'COMPLETE'")).
		WithArgs(9, 4, 25, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "audit_id", "audit", "number", "desc", "status", "due", "team", "uuid", "type", "team_id", "owner_id",
		}).AddRow(1, 4, "SOC2", "CC1.1", "d", "COMPLETE", "2026-01-01", "Platform", "u1", "INTERNAL", 2, 5))

	page, err := repo.GetWorkQueuePage(context.Background(), domain.WorkQueueRequest{
		WorkQueueScope: domain.ScopeOwned, UserID: 9, Tab: domain.WorkQueueTabCompleted,
		Page: 1, Limit: 25, AuditIDs: []int{4},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Status != "COMPLETE" {
		t.Errorf("page = %+v, want one COMPLETE item", page)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
