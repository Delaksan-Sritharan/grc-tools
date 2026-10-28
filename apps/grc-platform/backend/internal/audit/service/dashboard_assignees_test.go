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

package service

import (
	"context"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/model"
)

// With no identity directory (local dev without SCIM), enrichAssignees must
// leave the rows exactly as the entity sent them — uuid intact, name empty —
// rather than panicking or blanking anything.
func TestEnrichAssignees_NilDirectoryLeavesRowsUntouched(t *testing.T) {
	s := &dashboardService{}
	rows := []model.AssigneeStatusCount{
		{Role: "owner", AssigneeUUID: "u1", AssigneeUserType: "INTERNAL", Status: "EVIDENCE_PENDING", Count: 2},
		{Role: "compliance", Assignee: "Platform", Status: "EVIDENCE_INTERNAL_REVIEW", Count: 1},
	}
	want := append([]model.AssigneeStatusCount(nil), rows...)

	s.enrichAssignees(context.Background(), rows)

	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d changed: %+v, want %+v", i, rows[i], want[i])
		}
	}
}

func TestEnrichAssignees_EmptyRows(t *testing.T) {
	(&dashboardService{}).enrichAssignees(context.Background(), nil) // must not panic
}
