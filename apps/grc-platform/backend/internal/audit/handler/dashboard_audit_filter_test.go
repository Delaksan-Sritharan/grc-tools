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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/service"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/middleware"
)

type captureDashboardService struct {
	service.DashboardService
	got    model.DashboardFilter
	called bool
}

func (c *captureDashboardService) Get(_ context.Context, f model.DashboardFilter) (*model.DashboardData, error) {
	c.called, c.got = true, f
	return &model.DashboardData{}, nil
}

// getDashboardAs calls the handler as an authenticated caller in local-dev
// allow-all mode (no privilege map), which grants ViewAudits and ScopeAll.
func getDashboardAs(t *testing.T, query string) (*httptest.ResponseRecorder, *captureDashboardService) {
	t.Helper()
	svc := &captureDashboardService{}
	h := &dashboardHandler{svc: svc}
	ctx := middleware.WithUserInfo(context.Background(), &middleware.UserInfo{Subject: "uuid", UserID: 1})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audits/dashboard"+query, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.getDashboard(rec, req)
	return rec, svc
}

func TestGetDashboard_AuditIDsPassedToService(t *testing.T) {
	rec, svc := getDashboardAs(t, "?auditIds=4&auditIds=7")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(svc.got.AuditIDs) != 2 || svc.got.AuditIDs[0] != 4 || svc.got.AuditIDs[1] != 7 {
		t.Errorf("AuditIDs = %v, want [4 7]", svc.got.AuditIDs)
	}
}

func TestGetDashboard_NoAuditIDsMeansUnfiltered(t *testing.T) {
	rec, svc := getDashboardAs(t, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(svc.got.AuditIDs) != 0 {
		t.Errorf("AuditIDs = %v, want none", svc.got.AuditIDs)
	}
}

func TestGetDashboard_RejectsBadAuditIDs(t *testing.T) {
	tooMany := "?" + strings.TrimSuffix(strings.Repeat("auditIds=1&", maxDashboardAuditIDs+1), "&")
	for name, q := range map[string]string{
		"not an integer": "?auditIds=abc",
		"zero":           "?auditIds=0",
		"negative":       "?auditIds=-3",
		"empty value":    "?auditIds=",
		"over the cap":   tooMany,
	} {
		t.Run(name, func(t *testing.T) {
			rec, svc := getDashboardAs(t, q)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if svc.called {
				t.Error("service was called despite a malformed filter")
			}
		})
	}
}

func TestGetDashboard_AtTheCapIsAccepted(t *testing.T) {
	q := "?" + strings.TrimSuffix(strings.Repeat("auditIds=1&", maxDashboardAuditIDs), "&")
	rec, svc := getDashboardAs(t, q)
	if rec.Code != http.StatusOK || len(svc.got.AuditIDs) != maxDashboardAuditIDs {
		t.Errorf("status = %d, ids = %d; want 200 and %d", rec.Code, len(svc.got.AuditIDs), maxDashboardAuditIDs)
	}
}
