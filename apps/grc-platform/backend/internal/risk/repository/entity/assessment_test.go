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

package entity

import (
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
)

func TestAssessmentChangeLog(t *testing.T) {
	cur := "2026-11-30"
	current := &model.RiskDetail{OwnerID: 11, ManagementApproverID: 12, ImplementationDate: &cur}
	i := func(n int) *int { return &n }
	s := func(v string) *string { return &v }

	t.Run("one entry per changed field", func(t *testing.T) {
		got := assessmentChangeLog(current, model.CreateAssessmentRequest{
			OwnerID: i(21), ManagementApproverID: i(22), ImplementationDate: s("2026-12-31"),
		})
		want := map[string][2]string{
			"owner_id":               {`"11"`, `"21"`},
			"management_approver_id": {`"12"`, `"22"`},
			"implementation_date":    {`"2026-11-30"`, `"2026-12-31"`},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
		}
		for _, e := range got {
			w, ok := want[e["fieldChanged"].(string)]
			if !ok || e["oldValue"] != w[0] || e["newValue"] != w[1] || e["action"] != "UPDATE" {
				t.Errorf("unexpected entry %v", e)
			}
		}
	})

	t.Run("unchanged and omitted fields log nothing", func(t *testing.T) {
		got := assessmentChangeLog(current, model.CreateAssessmentRequest{
			OwnerID: i(11), ImplementationDate: s("2026-11-30"),
		})
		if len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})
}
