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
	"reflect"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/entityclient"
)

// The entity answers in camelCase; a field that fails to map comes back as a
// zero value rather than an error, so the managed-services block is checked
// field by field.
func TestDashboardRepository_MapsManagedServices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/risk/dashboard/search" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"managedServices": map[string]any{
				"customerCount": 5, "openRisks": 16, "closedRisks": 2, "highOpen": 6, "highClosed": 1,
				"themes": []map[string]any{
					{"categoryId": 9, "categoryName": "Disaster Recovery", "customers": 4, "total": 4, "high": 1, "medium": 1, "low": 2},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	repo := &dashboardRepository{c: entityclient.New(srv.URL)}

	got, err := repo.Summary(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	want := model.ManagedServicesRisks{
		CustomerCount: 5, OpenRisks: 16, ClosedRisks: 2, HighOpen: 6, HighClosed: 1,
		Themes: []model.ManagedServicesTheme{
			{CategoryID: 9, CategoryName: "Disaster Recovery", Customers: 4, Total: 4, High: 1, Medium: 1, Low: 2},
		},
	}
	if !reflect.DeepEqual(got.ManagedServices, want) {
		t.Errorf("ManagedServices = %+v, want %+v", got.ManagedServices, want)
	}
}

// An entity that predates the field must still yield an empty, non-nil theme
// list, so the response serialises as [] rather than null.
func TestDashboardRepository_ManagedServicesAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	t.Cleanup(srv.Close)
	repo := &dashboardRepository{c: entityclient.New(srv.URL)}

	got, err := repo.Summary(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.ManagedServices.CustomerCount != 0 || got.ManagedServices.Themes == nil || len(got.ManagedServices.Themes) != 0 {
		t.Errorf("ManagedServices = %+v, want zero counts and an empty non-nil theme list", got.ManagedServices)
	}
}
