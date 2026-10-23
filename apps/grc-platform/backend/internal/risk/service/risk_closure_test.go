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
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
)

// needsManagementSignOff decides both management stages, so getting it wrong
// either strands non-HIGH risks at an approval nobody expects, or lets a HIGH
// risk close without the sign-off it exists to require. Treatment strategy does
// not matter.
func TestNeedsManagementSignOff(t *testing.T) {
	score := func(level string) *model.RiskScore { return &model.RiskScore{RiskLevel: level} }

	cases := []struct {
		name     string
		gross    *model.RiskScore
		current  *model.RiskScore // effective score; nil means no reassessment yet
		approver int
		want     bool
	}{
		{"high needs sign-off", score("HIGH"), nil, 8, true},
		{"medium does not", score("MEDIUM"), nil, 8, false},
		{"low does not", score("LOW"), nil, 0, false},
		// Defensive: no gross score must not be routed to management on a nil
		// dereference, and a HIGH risk with no approver on record would strand
		// at a stage nobody can act on, so it skips management instead.
		{"nil gross score does not", nil, nil, 8, false},
		// Reassessed up to HIGH: the approver the reassessment required must sign off.
		{"medium reassessed to high needs sign-off", score("MEDIUM"), score("HIGH"), 8, true},
		// Reassessed down: the approver named for the original HIGH still signs off.
		{"high reassessed to medium still needs sign-off", score("HIGH"), score("MEDIUM"), 8, true},
		{"medium reassessed to low does not", score("MEDIUM"), score("LOW"), 8, false},
		{"high with no approver does not", score("HIGH"), nil, 0, false},
		{"reassessed to high with no approver does not", score("MEDIUM"), score("HIGH"), 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			detail := &model.RiskDetail{GrossScore: c.gross, EffectiveScore: c.current, ManagementApproverID: c.approver}
			if got := needsManagementSignOff(detail); got != c.want {
				t.Errorf("needsManagementSignOff = %v, want %v", got, c.want)
			}
		})
	}
}
