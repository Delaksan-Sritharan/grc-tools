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
	"reflect"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/middleware"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/grant"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

// searchHistory captures the filter the audit-trail handler built.
type searchHistory struct {
	recordingHistory
	calls int
	got   model.AuditTrailFilter
}

func (s *searchHistory) Search(_ context.Context, f model.AuditTrailFilter) (*model.AuditTrailPage, error) {
	s.calls++
	s.got = f
	return &model.AuditTrailPage{Items: []*model.AuditTrailEntry{}}, nil
}

// analystCtx is a caller holding Analytics on one register, built from a real
// grant so team scoping (RiskScopeIDs) resolves as it does in production.
// ctxFor is a caller holding the given grants, built the way production builds
// it (grant.Resolve) so role names and team scoping both resolve. Each role
// carries the privileges the seed gives it that matter here.
func ctxFor(grants ...grant.Grant) context.Context {
	set := grant.Resolve(grants, stubResolver{
		roleRiskOwner:                        {privilege.ViewRisks, privilege.ViewAnalytics},
		roleRiskComplianceTeam:               {privilege.ViewRisks, privilege.ViewAllRisks, privilege.ViewAnalytics},
		"grc-platform-risk-assigner":         {privilege.ViewRisks, privilege.ViewAnalytics},
		"grc-platform-risk-management":       {privilege.ViewRisks, privilege.ViewAnalytics},
		"grc-platform-risk-compliance-admin": {privilege.ViewRisks, privilege.ViewAnalytics},
		"analytics-only":                     {privilege.ViewAnalytics},
	})
	ctx := middleware.WithUserInfo(context.Background(), &middleware.UserInfo{Subject: "test-caller-uuid"})
	ctx = grant.WithContext(ctx, set)
	return privilege.WithContext(ctx, set.PrivilegeMap())
}

func teamGrant(role string, registerID int) grant.Grant {
	return grant.Grant{
		RoleName: role, ScopeType: grant.ScopeRiskTeam, ScopeID: registerID,
		ScopeBasis: "SOURCE_REGISTER", ScopeTeamType: grant.TeamSourceRegister,
	}
}

func globalGrant(role string) grant.Grant {
	return grant.Grant{RoleName: role, ScopeType: grant.ScopeGlobal}
}

// Only risk owners and the compliance team may open the audit trail, and the
// risks it lists are scoped like the register: the compliance team (global
// view) is unrestricted, an owner is limited to their teams, and the query
// string can never widen that.
func TestHandleAuditTrail(t *testing.T) {
	cases := []struct {
		name       string
		ctx        context.Context
		url        string
		wantStatus int
		wantCalled bool
		wantFilter func(t *testing.T, f model.AuditTrailFilter)
	}{
		{name: "no grants at all", ctx: ctxFor(), url: "/api/v1/risks/audit-trail", wantStatus: http.StatusForbidden},
		{
			name: "assigner holds analytics but is not owner or compliance team",
			ctx:  ctxFor(teamGrant("grc-platform-risk-assigner", asgardeo)),
			url:  "/api/v1/risks/audit-trail", wantStatus: http.StatusForbidden,
		},
		{
			name: "compliance admin is not compliance team",
			ctx:  ctxFor(teamGrant("grc-platform-risk-compliance-admin", asgardeo)),
			url:  "/api/v1/risks/audit-trail", wantStatus: http.StatusForbidden,
		},
		{
			name: "management is not allowed",
			ctx:  ctxFor(teamGrant("grc-platform-risk-management", asgardeo)),
			url:  "/api/v1/risks/audit-trail", wantStatus: http.StatusForbidden,
		},
		{
			name: "compliance team is unscoped", ctx: ctxFor(globalGrant(roleRiskComplianceTeam)),
			url: "/api/v1/risks/audit-trail", wantStatus: http.StatusOK, wantCalled: true,
			wantFilter: func(t *testing.T, f model.AuditTrailFilter) {
				if f.ScopeSourceRegisterIDs != nil || f.ScopeAssignmentTeamIDs != nil || f.Limit != 50 {
					t.Errorf("unexpected scope or limit: %+v", f)
				}
			},
		},
		{
			name: "risk owner is scoped to their team", ctx: ctxFor(teamGrant(roleRiskOwner, asgardeo)),
			url:        "/api/v1/risks/audit-trail?action=update,escalate&from=2026-01-01&to=2026-01-31&search=ASG&limit=10&offset=20",
			wantStatus: http.StatusOK, wantCalled: true,
			wantFilter: func(t *testing.T, f model.AuditTrailFilter) {
				if !reflect.DeepEqual(f.ScopeSourceRegisterIDs, []int{asgardeo}) || !reflect.DeepEqual(f.ScopeAssignmentTeamIDs, []int{asgardeo}) {
					t.Errorf("scope = %v / %v, want [%d]", f.ScopeSourceRegisterIDs, f.ScopeAssignmentTeamIDs, asgardeo)
				}
				if f.From != "2026-01-01" || f.To != "2026-01-31" || f.RiskQuery != "ASG" || f.Limit != 10 || f.Offset != 20 ||
					!reflect.DeepEqual(f.Actions, []string{"update", "escalate"}) {
					t.Errorf("filters not passed through: %+v", f)
				}
				if f.EscalationLeadUUID != "test-caller-uuid" {
					t.Errorf("escalation lead = %q, want the caller's subject", f.EscalationLeadUUID)
				}
			},
		},
		{
			name: "owner also holding another role is still allowed, scoped by their teams",
			ctx:  ctxFor(teamGrant(roleRiskOwner, asgardeo), teamGrant("grc-platform-risk-assigner", choreo)),
			url:  "/api/v1/risks/audit-trail", wantStatus: http.StatusOK, wantCalled: true,
			wantFilter: func(t *testing.T, f model.AuditTrailFilter) {
				if !reflect.DeepEqual(f.ScopeSourceRegisterIDs, []int{asgardeo, choreo}) {
					t.Errorf("scope = %v, want both teams", f.ScopeSourceRegisterIDs)
				}
			},
		},
		{
			name: "oversized limit falls back to default", ctx: ctxFor(teamGrant(roleRiskOwner, asgardeo)),
			url: "/api/v1/risks/audit-trail?limit=500", wantStatus: http.StatusOK, wantCalled: true,
			wantFilter: func(t *testing.T, f model.AuditTrailFilter) {
				if f.Limit != 50 {
					t.Errorf("limit = %d, want 50", f.Limit)
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hist := &searchHistory{}
			d := &Deps{History: hist, Users: fakeUserRepo{uuid: "test-caller-uuid", id: 1}}
			req := httptest.NewRequest(http.MethodGet, c.url, nil).WithContext(c.ctx)
			rec := httptest.NewRecorder()

			d.handleAuditTrail(rec, req)

			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, c.wantStatus, rec.Body.String())
			}
			if called := hist.calls > 0; called != c.wantCalled {
				t.Fatalf("search called = %v, want %v", called, c.wantCalled)
			}
			if c.wantFilter != nil {
				c.wantFilter(t, hist.got)
			}
		})
	}
}

// A compliance-team member whose grant is global but who does not hold
// RISK_VIEW_RISKS globally cannot be treated as seeing every risk, and carries
// no team ids, so they must get an empty page and not an unscoped one.
func TestHandleAuditTrailFailsClosedWithoutTeams(t *testing.T) {
	hist := &searchHistory{}
	d := &Deps{History: hist}
	set := grant.Resolve([]grant.Grant{globalGrant(roleRiskComplianceTeam)},
		stubResolver{roleRiskComplianceTeam: {privilege.ViewAnalytics}})
	ctx := middleware.WithUserInfo(context.Background(), &middleware.UserInfo{Subject: "test-caller-uuid"})
	ctx = grant.WithContext(ctx, set)
	ctx = privilege.WithContext(ctx, set.PrivilegeMap())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risks/audit-trail", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	d.handleAuditTrail(rec, req)

	if rec.Code != http.StatusOK || hist.calls != 0 {
		t.Fatalf("status = %d, search calls = %d; want an empty 200 and no search", rec.Code, hist.calls)
	}
}

// The audit-trail page is reported to the frontend as a capability: true for
// the two roles, false for everyone else, so the nav hides it from the rest.
func TestCanViewAuditTrail(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want bool
	}{
		{"risk owner", ctxFor(teamGrant(roleRiskOwner, asgardeo)), true},
		{"compliance team", ctxFor(globalGrant(roleRiskComplianceTeam)), true},
		{"assigner", ctxFor(teamGrant("grc-platform-risk-assigner", asgardeo)), false},
		{"no grants", ctxFor(), false},
	}
	for _, c := range cases {
		if got := canViewAuditTrail(c.ctx); got != c.want {
			t.Errorf("%s: canViewAuditTrail = %v, want %v", c.name, got, c.want)
		}
	}
}
