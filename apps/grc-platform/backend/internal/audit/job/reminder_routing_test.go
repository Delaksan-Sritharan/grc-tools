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

package job

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/audit/model"
)

const (
	routeOwner    = 100
	routePopOwner = 101
	routeAuditor  = 102
	routeAdminA   = 900
	routeAdminB   = 901
)

// routeResult is what one sweep delivered, keyed by recipient.
type routeResult struct {
	reminders   map[int][]model.ReminderItem
	escalations map[int][]model.ReminderItem
	leads       map[int][]model.ReminderItem
}

// runRouting sweeps controls (all in one ACTIVE audit) with admins and leads
// wired, and records who was sent what.
func runRouting(t *testing.T, controls ...*model.AuditControl) routeResult {
	t.Helper()
	res := routeResult{
		reminders:   map[int][]model.ReminderItem{},
		escalations: map[int][]model.ReminderItem{},
		leads:       map[int][]model.ReminderItem{},
	}
	audits := &fakeAudits{audits: []*model.Audit{{ID: 1, Status: "ACTIVE", Name: "SOC 2"}}}
	names := func(_ context.Context, ids []int) map[int]string {
		out := map[int]string{}
		for _, id := range ids {
			out[id] = map[int]string{routeOwner: "Owner", routePopOwner: "Pop Owner", routeAuditor: "Auditor"}[id]
		}
		return out
	}
	leads := func(_ context.Context, ids []int) map[int]string {
		out := map[int]string{}
		for _, id := range ids {
			out[id] = "lead@x.com"
		}
		return out
	}
	j := NewReminderJob(audits, &fakeControls{controls: controls}, &fakeClaimer{claimed: map[string]bool{}},
		func(_ context.Context, id int, items []model.ReminderItem) error {
			res.reminders[id] = append(res.reminders[id], items...)
			return nil
		}).
		WithAdminAlerts(adminsFn(routeAdminA, routeAdminB),
			func(_ context.Context, id int, items []model.ReminderItem) error {
				res.escalations[id] = append(res.escalations[id], items...)
				return nil
			}, names).
		WithLeadAlerts(leads, func(_ context.Context, ownerID int, _ string, items []model.ReminderItem) error {
			res.leads[ownerID] = append(res.leads[ownerID], items...)
			return nil
		})
	if err := j.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	return res
}

func recipients(m map[int][]model.ReminderItem) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func daysFromToday(n int) *string {
	return strPtr(time.Now().UTC().AddDate(0, 0, n).Format("2006-01-02"))
}

// routedControl is a fully assigned control whose population is due in
// popOffset days and whose evidence is due in evidenceOffset days.
func routedControl(status string, popOffset, evidenceOffset int) *model.AuditControl {
	return &model.AuditControl{
		ID: 1, AuditID: 1, ControlNumber: "C-1", Status: status,
		OwnerID: intPtr(routeOwner), AuditorID: intPtr(routeAuditor), DueDate: daysFromToday(evidenceOffset),
		PopulationID: intPtr(50), PopulationOwnerID: intPtr(routePopOwner),
		PopulationStatus: strPtr("PENDING"), PopulationDueDate: daysFromToday(popOffset),
	}
}

// Every status routes its reminder to whoever must act next, measured against
// the population date in a population status and the evidence date otherwise.
func TestReminderGoesToNextActorByStatus(t *testing.T) {
	admins := []int{routeAdminA, routeAdminB}
	cases := []struct {
		status      string
		want        []int
		requirement string
	}{
		{"POPULATION_PENDING", []int{routePopOwner}, "Population Requirement"},
		{"POPULATION_NEED_CLARIFICATION", []int{routePopOwner}, "Population Requirement"},
		{"POPULATION_INTERNAL_REVIEW", admins, "Population Requirement"},
		{"POPULATION_UNDER_VALIDATION", []int{routeAuditor}, "Population Requirement"},
		{"POPULATION_COMPLETE", []int{routeAuditor}, "Evidence Requirement"},
		{"AWAITING_SAMPLE", []int{routeAuditor}, "Evidence Requirement"},
		{"SUBMITTED_SAMPLE", []int{routeOwner}, "Evidence Requirement"},
		{"EVIDENCE_PENDING", []int{routeOwner}, "Evidence Requirement"},
		{"EVIDENCE_NEED_CLARIFICATION", []int{routeOwner}, "Evidence Requirement"},
		{"EVIDENCE_INTERNAL_REVIEW", admins, "Evidence Requirement"},
		{"EVIDENCE_UNDER_VALIDATION", []int{routeAuditor}, "Evidence Requirement"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			// Only the date for the control's own phase is 5 days out; the
			// other one is far off, so a reminder off the wrong date sends nothing.
			popOffset, evidenceOffset := 5, 60
			if tc.requirement == "Evidence Requirement" {
				popOffset, evidenceOffset = -60, 5
			}
			res := runRouting(t, routedControl(tc.status, popOffset, evidenceOffset))
			if got := recipients(res.reminders); !slices.Equal(got, tc.want) {
				t.Fatalf("reminder recipients = %v, want %v", got, tc.want)
			}
			for id, items := range res.reminders {
				if len(items) != 1 || items[0].RequirementType != tc.requirement || items[0].Type != "REMINDER_DUE_5" {
					t.Errorf("recipient %d items = %+v, want one %s REMINDER_DUE_5", id, items, tc.requirement)
				}
			}
			if len(res.escalations) != 0 || len(res.leads) != 0 {
				t.Errorf("a not-yet-overdue item escalated: admins %v, leads %v", recipients(res.escalations), recipients(res.leads))
			}
		})
	}
}

func TestCompleteControlRemindsNobody(t *testing.T) {
	c := routedControl("COMPLETE", -3, -3)
	c.PopulationStatus = strPtr("APPROVED")
	res := runRouting(t, c)
	if len(res.reminders)+len(res.escalations)+len(res.leads) != 0 {
		t.Errorf("COMPLETE control sent reminders %v, escalations %v, leads %v",
			recipients(res.reminders), recipients(res.escalations), recipients(res.leads))
	}
}

// Overdue: the next actor keeps their reminder, every admin gets the
// escalation, and a lead only hears about an owner's own submission.
func TestOverdueEscalation(t *testing.T) {
	admins := []int{routeAdminA, routeAdminB}
	cases := []struct {
		status    string
		reminded  []int
		lead      []int
		waitingOn string
	}{
		{"POPULATION_PENDING", []int{routePopOwner}, []int{routePopOwner}, "Pop Owner"},
		{"EVIDENCE_PENDING", []int{routeOwner}, []int{routeOwner}, "Owner"},
		{"SUBMITTED_SAMPLE", []int{routeOwner}, []int{routeOwner}, "Owner"},
		{"EVIDENCE_INTERNAL_REVIEW", nil, nil, "Compliance Admins"},
		{"POPULATION_INTERNAL_REVIEW", nil, nil, "Compliance Admins"},
		{"EVIDENCE_UNDER_VALIDATION", []int{routeAuditor}, nil, "Auditor"},
		{"AWAITING_SAMPLE", []int{routeAuditor}, nil, "Auditor"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			res := runRouting(t, routedControl(tc.status, -2, -2))
			if got := recipients(res.reminders); !slices.Equal(got, tc.reminded) {
				t.Errorf("overdue reminder recipients = %v, want %v", got, tc.reminded)
			}
			if got := recipients(res.escalations); !slices.Equal(got, admins) {
				t.Fatalf("escalation recipients = %v, want every admin %v", got, admins)
			}
			for id, items := range res.escalations {
				if len(items) != 1 || items[0].WaitingOn != tc.waitingOn || items[0].Status == "" {
					t.Errorf("admin %d escalation = %+v, want one item waiting on %q with a status", id, items, tc.waitingOn)
				}
			}
			if got := recipients(res.leads); !slices.Equal(got, tc.lead) {
				t.Errorf("lead escalations for owners %v, want %v", got, tc.lead)
			}
		})
	}
}

// With nobody assigned the admins stand in: reminded ahead of the due date,
// and once overdue told only through the escalation digest.
func TestUnassignedNextActorFallsToAdmins(t *testing.T) {
	admins := []int{routeAdminA, routeAdminB}
	cases := []struct {
		status string
		note   string
	}{
		{"POPULATION_PENDING", "No owner assigned"},
		{"EVIDENCE_PENDING", "No owner assigned"},
		{"EVIDENCE_UNDER_VALIDATION", "No auditor assigned"},
	}
	for _, tc := range cases {
		t.Run(tc.status+"/due soon", func(t *testing.T) {
			c := routedControl(tc.status, 5, 5)
			c.OwnerID, c.PopulationOwnerID, c.AuditorID = nil, nil, nil
			res := runRouting(t, c)
			if got := recipients(res.reminders); !slices.Equal(got, admins) {
				t.Fatalf("reminder recipients = %v, want every admin %v", got, admins)
			}
			for id, items := range res.reminders {
				if len(items) != 1 || items[0].UnassignedNote != tc.note {
					t.Errorf("admin %d reminder = %+v, want one item noted %q", id, items, tc.note)
				}
			}
			if len(res.escalations) != 0 {
				t.Errorf("not-yet-overdue unassigned item escalated to %v", recipients(res.escalations))
			}
		})
		t.Run(tc.status+"/overdue", func(t *testing.T) {
			c := routedControl(tc.status, -2, -2)
			c.OwnerID, c.PopulationOwnerID, c.AuditorID = nil, nil, nil
			res := runRouting(t, c)
			if len(res.reminders) != 0 {
				t.Errorf("overdue unassigned item also sent reminders to %v, want escalation only", recipients(res.reminders))
			}
			if got := recipients(res.escalations); !slices.Equal(got, admins) {
				t.Fatalf("escalation recipients = %v, want every admin %v", got, admins)
			}
			for id, items := range res.escalations {
				if len(items) != 1 || items[0].WaitingOn != "Unassigned" {
					t.Errorf("admin %d escalation = %+v, want one item waiting on Unassigned", id, items)
				}
			}
			if len(res.leads) != 0 {
				t.Errorf("unassigned item escalated to a lead: %v", recipients(res.leads))
			}
		})
	}
}

// A control still in its population phase is not also reminded about its
// evidence date, and once the population is done its date stops applying.
func TestOnlyThePhaseDueDateApplies(t *testing.T) {
	t.Run("evidence date ignored during population phase", func(t *testing.T) {
		res := runRouting(t, routedControl("POPULATION_PENDING", 60, -2))
		if len(res.reminders)+len(res.escalations) != 0 {
			t.Errorf("overdue evidence date fired during the population phase: reminders %v, escalations %v",
				recipients(res.reminders), recipients(res.escalations))
		}
	})
	t.Run("population date ignored after population phase", func(t *testing.T) {
		res := runRouting(t, routedControl("EVIDENCE_PENDING", -2, 60))
		if len(res.reminders)+len(res.escalations) != 0 {
			t.Errorf("overdue population date fired in the evidence phase: reminders %v, escalations %v",
				recipients(res.reminders), recipients(res.escalations))
		}
	})
}

// An admin the overdue item is waiting on hears about it in their own
// reminder, not a second time in the escalation.
func TestAdminWhoIsNextActorIsNotAlsoEscalatedTo(t *testing.T) {
	c := routedControl("EVIDENCE_PENDING", -2, -2)
	c.OwnerID = intPtr(routeAdminA)
	res := runRouting(t, c)
	if got := recipients(res.reminders); !slices.Equal(got, []int{routeAdminA}) {
		t.Errorf("reminder recipients = %v, want [%d]", got, routeAdminA)
	}
	if got := recipients(res.escalations); !slices.Equal(got, []int{routeAdminB}) {
		t.Errorf("escalation recipients = %v, want only the other admin [%d]", got, routeAdminB)
	}
}

// One person can be an owner on one control and the auditor on another: their
// lead hears only about the submission they owe, not the item they are auditing.
func TestLeadEscalationExcludesItemsTheOwnerIsOnlyAuditing(t *testing.T) {
	owed := routedControl("EVIDENCE_PENDING", -2, -2)
	auditing := routedControl("EVIDENCE_UNDER_VALIDATION", -2, -2)
	auditing.ID, auditing.ControlNumber, auditing.AuditorID = 2, "C-2", intPtr(routeOwner)
	res := runRouting(t, owed, auditing)

	if got := len(res.reminders[routeOwner]); got != 2 {
		t.Fatalf("owner reminder items = %d, want 2 (one owed, one auditing)", got)
	}
	lead := res.leads[routeOwner]
	if len(lead) != 1 || lead[0].ControlNumber != "C-1" {
		t.Errorf("lead escalation = %+v, want only C-1 (the owed submission)", lead)
	}
}

// With no admins to stand in, admin-bound items reach nobody, but everyone
// else's reminders still go out.
func TestNoAdminsStillRemindsOwners(t *testing.T) {
	owned := routedControl("EVIDENCE_PENDING", -2, -2)
	inReview := routedControl("EVIDENCE_INTERNAL_REVIEW", -2, -2)
	inReview.ID = 2
	var reminded []int
	j := NewReminderJob(
		&fakeAudits{audits: []*model.Audit{{ID: 1, Status: "ACTIVE"}}},
		&fakeControls{controls: []*model.AuditControl{owned, inReview}},
		&fakeClaimer{claimed: map[string]bool{}},
		func(_ context.Context, id int, _ []model.ReminderItem) error {
			reminded = append(reminded, id)
			return nil
		}).
		WithAdminAlerts(adminsFn(), func(context.Context, int, []model.ReminderItem) error {
			t.Error("escalation sent with no admins")
			return nil
		}, nil)
	if err := j.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if !slices.Equal(reminded, []int{routeOwner}) {
		t.Errorf("reminded = %v, want only the owner [%d]", reminded, routeOwner)
	}
}

func TestEscalationNamesAnUnresolvablePerson(t *testing.T) {
	var got []model.ReminderItem
	j := NewReminderJob(
		&fakeAudits{audits: []*model.Audit{{ID: 1, Status: "ACTIVE"}}},
		&fakeControls{controls: []*model.AuditControl{routedControl("EVIDENCE_PENDING", -2, -2)}},
		&fakeClaimer{claimed: map[string]bool{}},
		func(context.Context, int, []model.ReminderItem) error { return nil }).
		WithAdminAlerts(adminsFn(routeAdminA), func(_ context.Context, _ int, items []model.ReminderItem) error {
			got = items
			return nil
		}, func(context.Context, []int) map[int]string { return map[int]string{} })
	if err := j.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if len(got) != 1 || got[0].WaitingOn != "Name unavailable" {
		t.Errorf("escalation = %+v, want one item waiting on \"Name unavailable\"", got)
	}
}
