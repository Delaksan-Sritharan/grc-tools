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
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/repository"
)

// stubAuditRepo serves a fixed audit set and records the last Update; the rest
// of the interface is unused by the rename paths.
type stubAuditRepo struct {
	repository.AuditRepository
	audits  []*model.Audit
	updated *model.UpdateAuditRequest
}

func (s *stubAuditRepo) List(context.Context) ([]*model.Audit, error) { return s.audits, nil }
func (s *stubAuditRepo) GetByID(_ context.Context, id int) (*model.Audit, error) {
	for _, a := range s.audits {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, nil
}
func (s *stubAuditRepo) Update(_ context.Context, _ int, req model.UpdateAuditRequest, _ string) error {
	s.updated = &req
	return nil
}

func ptr(s string) *string { return &s }

func newRenameService(audits ...*model.Audit) (AuditService, *stubAuditRepo) {
	repo := &stubAuditRepo{audits: audits}
	return NewAuditService(repo, nil, nil, nil), repo
}

func statusOf(err error) int {
	var ae *apierror.Error
	if errors.As(err, &ae) {
		return ae.StatusCode
	}
	return 0
}

func TestRenameSetsDisplayNameOnActiveAudit(t *testing.T) {
	svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "ACTIVE"})
	if err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{DisplayName: ptr("  AON  ")}, "u"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if repo.updated == nil || repo.updated.DisplayName == nil || *repo.updated.DisplayName != "AON" {
		t.Fatalf("want trimmed displayName AON sent to the repo, got %+v", repo.updated)
	}
	if repo.updated.Name != nil {
		t.Fatalf("a rename must never send the storage name, got %q", *repo.updated.Name)
	}
}

func TestRenameRejectsNonActiveAudits(t *testing.T) {
	for _, status := range []string{"COMPLETED", "ARCHIVED", "REMOVED"} {
		svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: status})
		err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{DisplayName: ptr("AON")}, "u")
		if statusOf(err) != http.StatusConflict {
			t.Fatalf("%s: want 409, got %v", status, err)
		}
		if repo.updated != nil {
			t.Fatalf("%s: repo must not be written", status)
		}
	}
}

func TestRenameRejectsDuplicateCaseInsensitive(t *testing.T) {
	display := "SOC 2 Prod"
	svc, repo := newRenameService(
		&model.Audit{ID: 1, Name: "Test", Status: "ACTIVE"},
		&model.Audit{ID: 2, Name: "AON", Status: "COMPLETED"},
		&model.Audit{ID: 3, Name: "Other", DisplayName: &display, Status: "ACTIVE"},
	)
	for _, name := range []string{"aon", "AON", "soc 2 prod"} {
		err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{DisplayName: ptr(name)}, "u")
		var ae *apierror.Error
		if !errors.As(err, &ae) || ae.StatusCode != http.StatusConflict || ae.Body != "Audit name already exists" {
			t.Fatalf("%q: want 409 'Audit name already exists', got %v", name, err)
		}
	}
	if repo.updated != nil {
		t.Fatal("repo must not be written on a duplicate")
	}
}

func TestRenameValidatesDisplayName(t *testing.T) {
	svc, _ := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "ACTIVE"})
	for _, name := range []string{"", "   ", strings.Repeat("a", 201), "bad\x00name", "line\nbreak"} {
		err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{DisplayName: ptr(name)}, "u")
		if statusOf(err) != http.StatusUnprocessableEntity {
			t.Fatalf("%q: want 422, got %v", name, err)
		}
	}
}

// Resubmitting the current effective name is a no-op: it is not written, and it
// is allowed on any status so a broader "save all fields" update never fails.
func TestRenameSameNameIsNoOp(t *testing.T) {
	svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "COMPLETED"})
	if err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{DisplayName: ptr("Test")}, "u"); err != nil {
		t.Fatalf("no-op rename: %v", err)
	}
	if repo.updated == nil || repo.updated.DisplayName != nil {
		t.Fatalf("no-op must not write displayName, got %+v", repo.updated)
	}
}

// Existing flows are untouched: updates without displayName behave as before,
// on any status, and the storage name stays locked.
func TestUpdateWithoutDisplayNameUnchanged(t *testing.T) {
	svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "COMPLETED"})
	if err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{Status: ptr("ARCHIVED")}, "u"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if repo.updated == nil || repo.updated.DisplayName != nil {
		t.Fatalf("status-only update must not carry displayName, got %+v", repo.updated)
	}
	err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{Name: ptr("Other")}, "u")
	if statusOf(err) != http.StatusConflict {
		t.Fatalf("storage name must stay locked, got %v", err)
	}
}

func TestEffectiveNameFallsBackToName(t *testing.T) {
	a := &model.Audit{Name: "Test"}
	if a.EffectiveName() != "Test" {
		t.Fatalf("nil displayName: want Test, got %q", a.EffectiveName())
	}
	a.DisplayName = ptr("AON")
	if a.EffectiveName() != "AON" || a.Name != "Test" {
		t.Fatalf("want AON shown and Test kept as storage name, got %q / %q", a.EffectiveName(), a.Name)
	}
}

// A rename must never move evidence: paths are built from the storage name.
func TestFolderPathsIgnoreDisplayName(t *testing.T) {
	a := &model.Audit{Name: "Test", DisplayName: ptr("AON")}
	if got := evidenceFolderPath(a.Name, "C-1"); got != "audit/Test/C-1/evidence/" {
		t.Fatalf("got %q", got)
	}
}

func TestEditPeriodAndScopeOnActiveAudit(t *testing.T) {
	svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "ACTIVE", PeriodStart: "2026-07-20", PeriodEnd: "2026-11-20"})
	req := model.UpdateAuditRequest{PeriodEnd: ptr("2026-12-31"), ScopeDescription: ptr("  Production only ")}
	if err := svc.Update(context.Background(), 1, req, "u"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if repo.updated == nil || repo.updated.PeriodEnd == nil || *repo.updated.PeriodEnd != "2026-12-31" {
		t.Fatalf("periodEnd not sent: %+v", repo.updated)
	}
	if repo.updated.ScopeDescription == nil || *repo.updated.ScopeDescription != "Production only" {
		t.Fatalf("scope not trimmed/sent: %+v", repo.updated)
	}
}

func TestEditPeriodRejectsBadRangeAndFormat(t *testing.T) {
	svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "ACTIVE", PeriodStart: "2026-07-20", PeriodEnd: "2026-11-20"})
	for name, req := range map[string]model.UpdateAuditRequest{
		"end before start":         {PeriodEnd: ptr("2026-07-01")},
		"start after existing end": {PeriodStart: ptr("2027-01-01")},
		"bad format":               {PeriodStart: ptr("20/07/2026")},
	} {
		if err := svc.Update(context.Background(), 1, req, "u"); statusOf(err) != http.StatusUnprocessableEntity {
			t.Fatalf("%s: want 422, got %v", name, err)
		}
	}
	if repo.updated != nil {
		t.Fatal("repo must not be written for an invalid period")
	}
}

func TestEditPeriodAndScopeRejectedForNonActive(t *testing.T) {
	svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "COMPLETED", PeriodStart: "2026-07-20", PeriodEnd: "2026-11-20"})
	if err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{PeriodEnd: ptr("2026-12-31")}, "u"); statusOf(err) != http.StatusConflict {
		t.Fatalf("period: want 409, got %v", err)
	}
	if err := svc.Update(context.Background(), 1, model.UpdateAuditRequest{ScopeDescription: ptr("x")}, "u"); statusOf(err) != http.StatusConflict {
		t.Fatalf("scope: want 409, got %v", err)
	}
	if repo.updated != nil {
		t.Fatal("repo must not be written")
	}
}

// Resubmitting the current period and scope is a no-op on any status.
func TestEditUnchangedPeriodAndScopeIsNoOp(t *testing.T) {
	scope := "Production only"
	svc, repo := newRenameService(&model.Audit{ID: 1, Name: "Test", Status: "ARCHIVED", PeriodStart: "2026-07-20", PeriodEnd: "2026-11-20", ScopeDescription: &scope})
	req := model.UpdateAuditRequest{PeriodStart: ptr("2026-07-20"), PeriodEnd: ptr("2026-11-20"), ScopeDescription: ptr("Production only")}
	if err := svc.Update(context.Background(), 1, req, "u"); err != nil {
		t.Fatalf("no-op: %v", err)
	}
	if repo.updated == nil || repo.updated.PeriodStart != nil || repo.updated.PeriodEnd != nil || repo.updated.ScopeDescription != nil {
		t.Fatalf("unchanged fields must not be written: %+v", repo.updated)
	}
}
