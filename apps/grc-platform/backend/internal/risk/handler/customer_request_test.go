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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

// Register 7 is on the Managed Services template, register 3 is not.
func customerRequestDeps() *Deps {
	return &Deps{Team: &fakeTeamService{teams: []*model.Team{
		{ID: 3, Name: "Asgardeo", TeamType: "BOTH", RegisterTemplate: model.TemplateStandard},
		{ID: 7, Name: "Managed Services", TeamType: "BOTH", RegisterTemplate: model.TemplateManagedServices},
	}}}
}

func postCustomerRequest(t *testing.T, d *Deps, byTeam map[int]map[string]bool, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/risks/customer-requests", strings.NewReader(body))
	req = req.WithContext(contextForGrants(t, nil, byTeam))
	rec := httptest.NewRecorder()
	d.handleRequestCustomer(rec, req)
	return rec
}

// Grants is nil in these Deps, so an accepted request stops before the send:
// these tests cover who may ask and what they may send, not the email.
func TestHandleRequestCustomer(t *testing.T) {
	createIn := func(id int) map[int]map[string]bool {
		return map[int]map[string]bool{id: {privilege.CreateRisk: true}}
	}
	cases := []struct {
		name   string
		grants map[int]map[string]bool
		body   string
		want   int
	}{
		{"assigner in a Managed Services register", createIn(7), `{"customer_name":"Acme Corp","suggested_code":"ACME","note":"new contract"}`, http.StatusAccepted},
		{"name only", createIn(7), `{"customer_name":"Acme Corp"}`, http.StatusAccepted},
		{"assigner only in a Standard register", createIn(3), `{"customer_name":"Acme Corp"}`, http.StatusForbidden},
		{"no grants", nil, `{"customer_name":"Acme Corp"}`, http.StatusForbidden},
		{"blank name", createIn(7), `{"customer_name":"   "}`, http.StatusBadRequest},
		{"lowercase code", createIn(7), `{"customer_name":"Acme","suggested_code":"acme"}`, http.StatusBadRequest},
		{"code with hyphen", createIn(7), `{"customer_name":"Acme","suggested_code":"AC-ME"}`, http.StatusBadRequest},
		{"note too long", createIn(7), `{"customer_name":"Acme","note":"` + strings.Repeat("x", maxCustomerNoteLen+1) + `"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postCustomerRequest(t, customerRequestDeps(), tc.grants, tc.body)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestCustomerRequestLimiter(t *testing.T) {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	l := &customerRequestLimiter{}

	if !l.allow("u1", "Acme Corp", now) {
		t.Fatal("first request should be allowed")
	}
	if l.allow("u1", "acme corp", now.Add(time.Minute)) {
		t.Error("same name (any case) within the window should be refused")
	}
	if !l.allow("u2", "Acme Corp", now.Add(time.Minute)) {
		t.Error("another requester must not share u1's limit")
	}
	if !l.allow("u1", "Acme Corp", now.Add(customerRequestWindow)) {
		t.Error("same name after the window should be allowed again")
	}

	l2 := &customerRequestLimiter{}
	for i := 0; i < customerRequestPerUser; i++ {
		if !l2.allow("u1", fmt.Sprintf("Customer %d", i), now) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l2.allow("u1", "One More", now) {
		t.Error("request over the per-user ceiling should be refused")
	}
	if !l2.allow("u1", "One More", now.Add(customerRequestWindow)) {
		t.Error("ceiling should clear once the window passes")
	}
}

func TestHandleRequestCustomerRateLimited(t *testing.T) {
	d := customerRequestDeps()
	d.customerRequests = &customerRequestLimiter{}
	grants := map[int]map[string]bool{7: {privilege.CreateRisk: true}}
	body := `{"customer_name":"Acme Corp"}`
	if rec := postCustomerRequest(t, d, grants, body); rec.Code != http.StatusAccepted {
		t.Fatalf("first request status = %d, want 202", rec.Code)
	}
	if rec := postCustomerRequest(t, d, grants, body); rec.Code != http.StatusTooManyRequests {
		t.Errorf("repeat request status = %d, want 429", rec.Code)
	}
}
