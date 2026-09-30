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
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/emailer"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/grant"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/user"

	riskservice "github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/service"
)

// panickingRiskSvc stands in for a dependency that panics partway through a
// send — a nil pointer, a bad type assertion, anything unexpected. The rest
// of Deps is never reached once GetByID panics.
type panickingRiskSvc struct{ riskservice.RiskService }

func (panickingRiskSvc) GetByID(context.Context, int) (*model.RiskDetail, error) {
	panic("simulated failure deep in a dependency")
}

// reminderUsers resolves ids to user rows so personLabel has something to
// work with. Directory stays nil, so labels come back as bare (empty) names —
// enough for the role-membership assertions below, which are about WHICH
// roles get listed, not how a person is rendered.
type reminderUsers struct{ user.Repository }

func (reminderUsers) GetByID(_ context.Context, id int) (*user.User, error) {
	return &user.User{ID: id, UUID: "uuid", Status: "ACTIVE"}, nil
}

// stubGrants answers Candidates with a fixed set, and records what it was
// asked for — the privilege and the register scope are both load-bearing.
type stubGrants struct {
	grant.Repository
	candidates    []grant.Candidate
	err           error
	gotPrivilege  string
	gotTeamIDs    []int
	candidateCall int
}

func (s *stubGrants) Candidates(_ context.Context, privilegeName string, teamIDs []int) ([]grant.Candidate, error) {
	s.candidateCall++
	s.gotPrivilege = privilegeName
	s.gotTeamIDs = teamIDs
	return s.candidates, s.err
}

func detailFor(assignerID, ownerID, registerID int) *model.RiskDetail {
	return &model.RiskDetail{
		ID:               1,
		AssignerID:       assignerID,
		OwnerID:          ownerID,
		SourceRegisterID: registerID,
		// Never a recipient, whatever the tier.
		ManagementApproverID: 99,
	}
}

func plan(ownerID int, status string) *model.ActionPlan {
	return &model.ActionPlan{ActionOwnerID: &ownerID, Status: status}
}

// The recipient rules, tier by tier. The Risk Owner joins only on the due
// date; the compliance roles are on every tier; the Management Approver never
// is.
func TestReminderRecipients_ByTier(t *testing.T) {
	grants := &stubGrants{candidates: []grant.Candidate{{ID: 50}, {ID: 51}}}
	d := &Deps{Grants: grants}
	detail := detailFor(10, 20, 7)
	plans := []*model.ActionPlan{plan(30, "IN_PROGRESS")}

	cases := []struct {
		tier string
		want []int
	}{
		{model.ReminderDueIn15Days, []int{10, 30, 50, 51}},
		{model.ReminderDueIn5Days, []int{10, 30, 50, 51}},
		{model.ReminderDueToday, []int{10, 30, 20, 50, 51}},
	}

	for _, c := range cases {
		t.Run(c.tier, func(t *testing.T) {
			got := d.reminderRecipients(context.Background(), detail, plans, c.tier)
			if !slices.Equal(got, c.want) {
				t.Fatalf("recipients = %v, want %v", got, c.want)
			}
			if slices.Contains(got, detail.ManagementApproverID) {
				t.Error("the Management Approver must never be reminded")
			}
		})
	}
}

// An Action Owner with nothing left to do is not reminded; one with any
// unfinished plan is. Someone who owns both is still in.
func TestReminderRecipients_SkipsOwnersWhoseWorkIsDone(t *testing.T) {
	d := &Deps{}
	detail := detailFor(10, 20, 7)

	plans := []*model.ActionPlan{
		plan(31, "COMPLETED"),   // done — not reminded
		plan(32, "IN_PROGRESS"), // still working
		plan(33, "PENDING"),     // not started
		plan(34, "COMPLETED"),   // done, but...
		plan(34, "PENDING"),     // ...also owns unfinished work
	}

	got := d.reminderRecipients(context.Background(), detail, plans, model.ReminderDueIn5Days)

	if slices.Contains(got, 31) {
		t.Error("owner 31's plans are all COMPLETED and must not be reminded")
	}
	for _, want := range []int{32, 33, 34} {
		if !slices.Contains(got, want) {
			t.Errorf("owner %d has unfinished work and must be reminded, got %v", want, got)
		}
	}
	// The Assigner is reminded regardless — they still have to submit for
	// completion approval before the date.
	if !slices.Contains(got, 10) {
		t.Errorf("the Assigner must always be reminded, got %v", got)
	}
}

// Even with every plan complete, the Assigner is still on the hook.
func TestReminderRecipients_AssignerRemindedWhenAllPlansComplete(t *testing.T) {
	d := &Deps{}
	got := d.reminderRecipients(context.Background(), detailFor(10, 20, 7),
		[]*model.ActionPlan{plan(31, "COMPLETED")}, model.ReminderDueIn15Days)

	if !slices.Equal(got, []int{10}) {
		t.Fatalf("recipients = %v, want just the Assigner", got)
	}
}

// The compliance lookup must ask for RISK_VIEW_ALL_RISKS — the privilege the
// Risk Compliance Team and Risk Compliance Admin hold and no other role does —
// scoped to the risk's own register, so a register-scoped grant elsewhere
// doesn't pull someone in.
func TestReminderRecipients_ResolvesComplianceByPrivilegeAndRegister(t *testing.T) {
	grants := &stubGrants{candidates: []grant.Candidate{{ID: 50}}}
	d := &Deps{Grants: grants}

	d.reminderRecipients(context.Background(), detailFor(10, 20, 7), nil, model.ReminderDueToday)

	if grants.gotPrivilege != privilege.ViewAllRisks {
		t.Errorf("privilege = %q, want %q", grants.gotPrivilege, privilege.ViewAllRisks)
	}
	if !slices.Equal(grants.gotTeamIDs, []int{7}) {
		t.Errorf("scope = %v, want the risk's register [7]", grants.gotTeamIDs)
	}
}

// A failed grant lookup must not lose the reminder: the people who can act on
// it still get told, without the oversight copy.
func TestReminderRecipients_SurvivesAFailedGrantLookup(t *testing.T) {
	grants := &stubGrants{err: errors.New("entity unreachable")}
	d := &Deps{Grants: grants}

	got := d.reminderRecipients(context.Background(), detailFor(10, 20, 7),
		[]*model.ActionPlan{plan(30, "PENDING")}, model.ReminderDueIn5Days)

	if !slices.Equal(got, []int{10, 30}) {
		t.Fatalf("recipients = %v, want the assigner and action owner", got)
	}
}

// Local dev has no privilege store at all; that must be a no-op rather than a
// nil dereference.
func TestReminderRecipients_NoGrantsConfigured(t *testing.T) {
	d := &Deps{}
	got := d.reminderRecipients(context.Background(), detailFor(10, 20, 7), nil, model.ReminderDueToday)
	if !slices.Equal(got, []int{10, 20}) {
		t.Fatalf("recipients = %v, want assigner and owner only", got)
	}
}

// "Who needs to act" names only the two roles with something to do. The
// compliance roles and (on the due date) the Risk Owner receive the email but
// are not listed there.
//
// Directory is nil here, so every label resolves empty — which also pins the
// second rule: a role nobody could be resolved for is left out entirely
// rather than rendering a blank row, and an empty block is nil so the
// template drops the whole section.
func TestReminderPeople_ListsOnlyActionableRoles(t *testing.T) {
	d := &Deps{Users: reminderUsers{}}
	people := d.reminderPeople(context.Background(), detailFor(10, 20, 7),
		[]*model.ActionPlan{plan(31, "COMPLETED"), plan(32, "PENDING")})

	for _, role := range []string{emailer.RoleRiskOwner, emailer.RoleManagementApprover} {
		if _, ok := people[role]; ok {
			t.Errorf("%q must not appear under 'who needs to act' — they are informed, not tasked", role)
		}
	}
	if people != nil {
		t.Errorf("people = %v, want nil when nobody resolves", people)
	}
}

// A tier with no event is a programming error and must fail loudly rather than
// send something generic.
func TestSendDueReminderSync_UnknownTier(t *testing.T) {
	d := &Deps{}
	err := d.SendDueReminderSync(context.Background(), 1, "DUE_IN_3_DAYS", "2026-10-06")
	if err == nil {
		t.Fatal("unknown tier was accepted")
	}
}

// The load-bearing property: a panic anywhere inside a single risk's send
// must come back as an error, not escape SendDueReminderSync. ReminderJob's
// per-risk loop has no panic isolation of its own — it relies entirely on
// this boundary — so a panic escaping here would abort the whole sweep
// (skipping every other risk still pending that run, on tiers that are never
// sent late) and leave this risk's claim un-released (bypassing runOnce's
// release-on-failure check), making that reminder permanently unclaimable.
func TestSendDueReminderSync_RecoversFromPanic(t *testing.T) {
	d := &Deps{Risk: panickingRiskSvc{}}

	err := d.SendDueReminderSync(context.Background(), 1, model.ReminderDueToday, "2026-10-06")

	if err == nil {
		t.Fatal("panic escaped SendDueReminderSync instead of being converted to an error")
	}
}

// The semaphore must be released even when the call panics, or every
// subsequent reminder (and every other notification sharing notifySem) blocks
// forever once notifyConcurrency panics have happened.
func TestSendDueReminderSync_ReleasesSemaphoreAfterPanic(t *testing.T) {
	d := &Deps{Risk: panickingRiskSvc{}}

	for i := 0; i < notifyConcurrency+1; i++ {
		done := make(chan struct{})
		go func() {
			_ = d.SendDueReminderSync(context.Background(), 1, model.ReminderDueToday, "2026-10-06")
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("call %d deadlocked — notifySem was not released after a panic", i)
		}
	}
}
