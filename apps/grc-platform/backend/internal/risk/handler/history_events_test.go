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

package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
)

func TestManagementApproveDetails(t *testing.T) {
	cases := []struct{ from, to string }{
		{model.StatusPendingManagementApproval, model.StatusPendingComplianceReview},
		{model.StatusPendingManagementClosure, model.StatusPendingComplianceClosure},
	}
	for _, c := range cases {
		got := managementApproveDetails(c.from)
		if got.Role != "Management" || got.From != c.from || got.To != c.to {
			t.Errorf("managementApproveDetails(%s) = %+v, want to=%s", c.from, got, c.to)
		}
	}
}

// The entity refuses an old/new value that is not valid JSON, and a refused
// history write is only logged, so every diff the handlers record must be
// encoded or the event silently never appears.
func TestEvidenceHistoryValuesAreValidJSON(t *testing.T) {
	var got []model.RecordHistoryRequest
	d := &Deps{History: &recordingHistory{onRecord: func(r model.RecordHistoryRequest) { got = append(got, r) }}}
	name := "quarterly report.pdf"

	d.recordEvidenceChange(context.Background(), 1, "by", nil, &name)
	d.recordEvidenceChange(context.Background(), 1, "by", &name, nil)

	if len(got) != 2 {
		t.Fatalf("recorded %d entries, want 2", len(got))
	}
	for _, r := range got {
		for _, v := range []*string{r.OldValue, r.NewValue} {
			if v != nil && !json.Valid([]byte(*v)) {
				t.Errorf("value %q is not valid JSON", *v)
			}
		}
	}
	if got[0].OldValue != nil || *got[0].NewValue != `"quarterly report.pdf"` {
		t.Errorf("upload = %v -> %v", got[0].OldValue, got[0].NewValue)
	}
	if got[1].NewValue != nil || *got[1].OldValue != `"quarterly report.pdf"` {
		t.Errorf("removal = %v -> %v", got[1].OldValue, got[1].NewValue)
	}
}

func TestJSONString(t *testing.T) {
	if v := jsonString(`COMPLETED`); v == nil || *v != `"COMPLETED"` {
		t.Errorf("jsonString = %v", v)
	}
	if jsonStringPtr(nil) != nil {
		t.Error("jsonStringPtr(nil) must stay nil")
	}
}
