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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

// Register 7 is on the Managed Services template, register 3 is not.
func TestCanCreateLookup(t *testing.T) {
	d := &Deps{Team: &fakeTeamService{teams: []*model.Team{
		{ID: 3, Name: "Asgardeo", TeamType: "BOTH", RegisterTemplate: model.TemplateStandard},
		{ID: 7, Name: "Managed Services", TeamType: "BOTH", RegisterTemplate: model.TemplateManagedServices},
	}}}
	createIn := func(id int) map[int]map[string]bool {
		return map[int]map[string]bool{id: {privilege.CreateRisk: true}}
	}
	cases := []struct {
		name   string
		path   string
		global map[string]bool
		byTeam map[int]map[string]bool
		want   bool
	}{
		{"assigner adds a customer", "customers", nil, createIn(7), true},
		{"assigner in a Standard register", "customers", nil, createIn(3), false},
		{"assigner adds a product", "products", nil, createIn(7), false},
		{"admin adds a customer", "customers", map[string]bool{privilege.ManageRiskHub: true}, nil, true},
		{"admin adds a product", "products", map[string]bool{privilege.ManageRiskHub: true}, nil, true},
		{"no grants", "customers", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risks/"+tc.path, nil)
			req = req.WithContext(contextForGrants(t, tc.global, tc.byTeam))
			rec := httptest.NewRecorder()
			if got := d.canCreateLookup(rec, req, lookupKind{path: tc.path}); got != tc.want {
				t.Errorf("canCreateLookup = %v, want %v", got, tc.want)
			}
		})
	}
}
