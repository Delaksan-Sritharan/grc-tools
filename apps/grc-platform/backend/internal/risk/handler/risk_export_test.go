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
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
)

// pagedRiskSvc serves its items by the filter's Limit/Offset, like the real
// list does, and records every filter it was called with.
type pagedRiskSvc struct {
	fakeRiskSvc
	items []*model.RiskListItem
	total int // overrides len(items) when non-zero
	calls []model.ListRisksFilter
}

func (p *pagedRiskSvc) List(_ context.Context, filter model.ListRisksFilter) (*model.RiskListPage, error) {
	p.calls = append(p.calls, filter)
	total := len(p.items)
	if p.total != 0 {
		total = p.total
	}
	lo := min(filter.Offset, len(p.items))
	hi := min(lo+filter.Limit, len(p.items))
	return &model.RiskListPage{Items: p.items[lo:hi], Total: total, Offset: filter.Offset, Limit: filter.Limit}, nil
}

func makeItems(n int) []*model.RiskListItem {
	items := make([]*model.RiskListItem, n)
	for i := range items {
		items[i] = &model.RiskListItem{ID: i + 1, RiskCode: fmt.Sprintf("R-%d", i+1), RiskTitle: "t"}
	}
	return items
}

func parseExport(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	body := strings.TrimPrefix(rec.Body.String(), "\xEF\xBB\xBF")
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("response is not valid CSV: %v", err)
	}
	return rows
}

func exportDeps(risk *pagedRiskSvc) *Deps {
	return &Deps{Risk: risk, Users: fakeUserRepo{uuid: "test-caller-uuid", id: 7}}
}

// TestHandleExportRisks_AllPages: every matching row comes back, however many
// pages that takes, and a client-supplied limit/offset is ignored.
func TestHandleExportRisks_AllPages(t *testing.T) {
	risk := &pagedRiskSvc{items: makeItems(450)}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risks/export?limit=5&offset=100", nil).
		WithContext(contextForGrants(t, map[string]bool{"RISK_VIEW_RISKS": true}, nil))
	rec := httptest.NewRecorder()

	exportDeps(risk).handleExportRisks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment; filename=risk-report-") {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.HasPrefix(rec.Body.String(), "\xEF\xBB\xBF") {
		t.Error("missing UTF-8 BOM")
	}
	rows := parseExport(t, rec)
	if len(rows) != 451 { // header + 450
		t.Fatalf("rows = %d, want 451", len(rows))
	}
	if rows[0][0] != "Risk Code" || rows[1][0] != "R-1" || rows[450][0] != "R-450" {
		t.Errorf("unexpected rows: %v / %v / %v", rows[0], rows[1], rows[450])
	}
	if len(risk.calls) != 3 {
		t.Fatalf("List called %d times, want 3", len(risk.calls))
	}
	for i, c := range risk.calls {
		if c.Limit != exportPageSize || c.Offset != i*exportPageSize {
			t.Errorf("call %d: limit/offset = %d/%d", i, c.Limit, c.Offset)
		}
	}
}

// TestHandleExportRisks_FiltersPassedThrough: the filters set in the UI reach
// the query on every page.
func TestHandleExportRisks_FiltersPassedThrough(t *testing.T) {
	risk := &pagedRiskSvc{items: makeItems(250)}
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/risks/export?statuses=APPROVED&level=HIGH,MEDIUM&treatment_strategy=ACCEPT&team_id=3&search=abc", nil).
		WithContext(contextForGrants(t, map[string]bool{"RISK_VIEW_RISKS": true}, nil))
	rec := httptest.NewRecorder()

	exportDeps(risk).handleExportRisks(rec, req)

	if len(risk.calls) != 2 {
		t.Fatalf("List called %d times, want 2", len(risk.calls))
	}
	for i, f := range risk.calls {
		if strings.Join(f.Statuses, ",") != "APPROVED" || strings.Join(f.Levels, ",") != "HIGH,MEDIUM" ||
			strings.Join(f.TreatmentStrategies, ",") != "ACCEPT" || len(f.TeamIDs) != 1 || f.TeamIDs[0] != 3 || f.Search != "abc" {
			t.Errorf("call %d lost a filter: %+v", i, f)
		}
	}
}

// TestHandleExportRisks_TeamScoped: export applies the same team scoping as the
// list, on every page.
func TestHandleExportRisks_TeamScoped(t *testing.T) {
	risk := &pagedRiskSvc{items: makeItems(250)}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risks/export", nil).
		WithContext(contextForRealGrants(t, ownerGrantOn(asgardeo)))
	rec := httptest.NewRecorder()

	exportDeps(risk).handleExportRisks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for i, f := range risk.calls {
		if !containsInt(f.ScopeSourceRegisterIDs, asgardeo) {
			t.Errorf("call %d ScopeSourceRegisterIDs = %v, want to contain %d", i, f.ScopeSourceRegisterIDs, asgardeo)
		}
	}
}

// TestHandleExportRisks_ActionOwnerNoGrants: a caller with no grants is scoped
// to risks they own an action plan on, exactly as in the list.
func TestHandleExportRisks_ActionOwnerNoGrants(t *testing.T) {
	risk := &pagedRiskSvc{items: makeItems(3)}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risks/export", nil).
		WithContext(contextForGrants(t, nil, nil))
	rec := httptest.NewRecorder()

	exportDeps(risk).handleExportRisks(rec, req)

	if len(risk.calls) == 0 || risk.calls[0].ActionOwnerID == nil || *risk.calls[0].ActionOwnerID != 7 {
		t.Fatalf("export was not scoped to the caller: %+v", risk.calls)
	}
}

// TestHandleExportRisks_NoGrantsNoUserRow: an unresolvable caller gets a
// header-only file and the service is never queried — never an unscoped export.
func TestHandleExportRisks_NoGrantsNoUserRow(t *testing.T) {
	risk := &pagedRiskSvc{items: makeItems(3)}
	d := &Deps{Risk: risk, Users: fakeUserRepo{uuid: "somebody-else"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risks/export", nil).
		WithContext(contextForGrants(t, nil, nil))
	rec := httptest.NewRecorder()

	d.handleExportRisks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(risk.calls) != 0 {
		t.Errorf("service queried %d times for a caller scoped to nothing", len(risk.calls))
	}
	if rows := parseExport(t, rec); len(rows) != 1 {
		t.Errorf("rows = %d, want header only", len(rows))
	}
}

// TestHandleExportRisks_TooManyRows: over the cap is an error, not a truncated file.
func TestHandleExportRisks_TooManyRows(t *testing.T) {
	risk := &pagedRiskSvc{items: makeItems(10), total: exportMaxRows + 1}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/risks/export", nil).
		WithContext(contextForGrants(t, map[string]bool{"RISK_VIEW_RISKS": true}, nil))
	rec := httptest.NewRecorder()

	exportDeps(risk).handleExportRisks(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if strings.Contains(rec.Header().Get("Content-Type"), "text/csv") {
		t.Error("an over-cap export must not be a CSV response")
	}
}

// TestRiskExportRecord covers the columns, including treatment strategy, and
// the quoting/escaping of awkward values.
func TestRiskExportRecord(t *testing.T) {
	ts, due := "ACCEPT", "2026-11-05T00:00:00Z"
	got := riskExportRecord(&model.RiskListItem{
		RiskCode: "R-1", RiskTitle: `=HYPERLINK("x")`, SourceRegisterName: "Asgardeo",
		RiskLevel: "HIGH", WorkflowStatus: "APPROVED", RiskType: "NEW",
		TreatmentStrategy: &ts, OwnerName: "A", AssignerName: "B",
		ImplementationDate: &due, CreatedAt: "2026-10-01T08:30:00.123456Z",
		PlatformNames: []string{"p1", "p2"}, Environments: []string{"PRODUCTION"},
	})
	want := []string{"R-1", `'=HYPERLINK("x")`, "Asgardeo", "HIGH", "APPROVED", "NEW", "ACCEPT", "A", "B",
		"2026-11-05", "2026-10-01", "", "p1; p2", "PRODUCTION", "", ""}
	if len(got) != len(riskExportHeader) {
		t.Fatalf("record has %d cells, header has %d", len(got), len(riskExportHeader))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cell %d (%s) = %q, want %q", i, riskExportHeader[i], got[i], want[i])
		}
	}
}

func TestCSVCell(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "plain": "plain", "=1+1": "'=1+1", "+1": "'+1", "-1": "'-1", "@x": "'@x",
		"\tx": "'\tx", "\rx": "'\rx", "a=b": "a=b",
	} {
		if got := csvCell(in); got != want {
			t.Errorf("csvCell(%q) = %q, want %q", in, got, want)
		}
	}
}
