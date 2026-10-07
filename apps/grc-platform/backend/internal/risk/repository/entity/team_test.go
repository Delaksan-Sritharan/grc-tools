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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/entityclient"
)

// The Admin Console disables a team's Template select once the team has risks,
// so has_risks must survive the trip from the entity's hasRisks to the
// backend's response — on the list and on create, which both return a team.
func TestTeamRepository_PassesHasRisksThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/risk/teams/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"total": 2, "teams": []map[string]any{
				{"id": 1, "name": "Used", "teamType": "BOTH", "registerTemplate": "MANAGED_SERVICES", "hasRisks": true, "status": "ACTIVE"},
				{"id": 2, "name": "Fresh", "teamType": "BOTH", "registerTemplate": "STANDARD", "hasRisks": false, "status": "ACTIVE"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/risk/teams":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 3, "name": "New", "teamType": "BOTH", "registerTemplate": "AGGREGATED", "hasRisks": false, "status": "ACTIVE"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	repo := &teamRepository{c: entityclient.New(srv.URL)}

	teams, err := repo.List(context.Background(), model.ListTeamsFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(teams) != 2 || !teams[0].HasRisks || teams[1].HasRisks {
		t.Errorf("has_risks not carried through the list: %+v %+v", teams[0], teams[1])
	}
	if teams[0].RegisterTemplate != "MANAGED_SERVICES" {
		t.Errorf("register_template = %q", teams[0].RegisterTemplate)
	}

	created, err := repo.Create(context.Background(), model.CreateTeamRequest{Name: "New", TeamType: "BOTH", RegisterTemplate: "AGGREGATED"}, "admin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.HasRisks || created.RegisterTemplate != "AGGREGATED" {
		t.Errorf("created = %+v", created)
	}
}
