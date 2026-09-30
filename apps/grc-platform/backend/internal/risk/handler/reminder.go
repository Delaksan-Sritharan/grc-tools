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
	"fmt"
	"log/slog"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/emailer"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

// completedPlanStatus is risk_action_plan.status's terminal value. A literal,
// as everywhere else that tests it (see service/risk.go's submit gate) — there
// is no shared constant for it yet.
const completedPlanStatus = "COMPLETED"

// reminderEvents maps a tier to the email it sends. A tier with no entry is a
// programming error, and SendDueReminderSync refuses it rather than sending
// something generic.
var reminderEvents = map[string]emailer.RiskEvent{
	model.ReminderDueIn15Days: emailer.EventDueIn15Days,
	model.ReminderDueIn5Days:  emailer.EventDueIn5Days,
	model.ReminderDueToday:    emailer.EventDueToday,
}

// SendDueReminderSync emails one risk's due-date reminder and reports whether
// it actually went out. Wired to job.ReminderJob.notify at startup: the sweep
// has already claimed this (risk, tier, due date), so a returned error is what
// makes it release the claim and leave the reminder retryable.
//
// Synchronous on purpose, unlike the workflow notifications, which detach into
// a goroutine so a handler can return its 200 without waiting: nothing is
// waiting on this, and the sweep must know the outcome before it decides
// whether to keep the claim.
//
// Goes through the same notifySem bound and notifyTimeout budget as
// sendRiskEventSync (the escalation job's equivalent), and for the same
// reason recovers its own panics into a returned error rather than letting
// them propagate: ReminderJob.runOnce calls this once per risk inside a single
// sweep, with no per-item isolation of its own. An unrecovered panic here
// would abort the whole sweep — silently skipping every risk still pending in
// it, on tiers that fire once and are never sent late — AND leave this risk's
// claim un-released, since the panic would bypass runOnce's
// release-on-failure check entirely, making that (risk, tier, due date)
// permanently unclaimable. Converting to an error keeps both failures scoped
// to this one risk.
func (d *Deps) SendDueReminderSync(ctx context.Context, riskID int, tier, dueDate string) (err error) {
	notifySem <- struct{}{}
	defer func() { <-notifySem }()
	defer func() {
		if p := recover(); p != nil {
			slog.Error("risk reminder: panic", "riskId", riskID, "tier", tier, "panic", p)
			err = fmt.Errorf("risk reminder panic: %v", p)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	return d.sendDueReminder(ctx, riskID, tier, dueDate)
}

// sendDueReminder is SendDueReminderSync's body, split out so the
// semaphore/timeout/recover wrapper above reads as a single, easily-audited
// shape — the same split sendRiskEventSync draws from sendRiskEvent.
func (d *Deps) sendDueReminder(ctx context.Context, riskID int, tier, dueDate string) error {
	ev, ok := reminderEvents[tier]
	if !ok {
		return fmt.Errorf("unknown reminder tier %q", tier)
	}

	detail, err := d.Risk.GetByID(ctx, riskID)
	if err != nil {
		slog.Warn("risk reminder: failed to load risk detail", "riskId", riskID, "tier", tier, "err", err)
		return fmt.Errorf("load risk detail: %w", err)
	}

	plans, err := d.ActionPlan.List(ctx, riskID)
	if err != nil {
		// Fatal for this send, unlike peopleForEvent's soft skip: the plans
		// decide who is reminded at all, so carrying on would email a subset
		// that looks complete but silently leaves out every Action Owner.
		slog.Warn("risk reminder: failed to list action plans", "riskId", riskID, "tier", tier, "err", err)
		return fmt.Errorf("list action plans: %w", err)
	}

	recipients := d.reminderRecipients(ctx, detail, plans, tier)
	emails := d.resolveRecipientEmails(ctx, ev, riskID, recipients)
	if len(emails) == 0 {
		// Nothing to do, and a retry would resolve the same empty set — so
		// this is an error for visibility, not a reason to release the claim
		// and try again tomorrow.
		slog.Warn("risk reminder: no deliverable recipients", "riskId", riskID, "tier", tier)
		return fmt.Errorf("no deliverable recipients")
	}

	level := ""
	if detail.EffectiveScore != nil {
		level = detail.EffectiveScore.RiskLevel
	}

	if err := d.Email.SendRiskEvent(ctx, ev, emails, emailer.RiskEventInfo{
		RiskCode:       detail.RiskCode,
		RiskTitle:      detail.RiskTitle,
		SourceRegister: detail.SourceRegisterName,
		// The effective (residual) level — the one this reminder's schedule
		// was decided on, and the one the register table shows. Deliberately
		// not the gross level the other risk emails print.
		RiskLevel: level,
		DueDate:   dueDate,
		// No Actor: a date passing is nobody's action.
		People:    d.reminderPeople(ctx, detail, plans),
		DetailURL: fmt.Sprintf("%s/risk/registers?riskId=%d", d.FrontendBaseURL, riskID),
	}); err != nil {
		slog.Warn("risk reminder: send failed", "riskId", riskID, "tier", tier, "recipients", len(emails), "err", err)
		return fmt.Errorf("send: %w", err)
	}
	slog.Info("risk reminder sent", "riskId", riskID, "tier", tier, "recipients", len(emails), "dueDate", dueDate)
	return nil
}

// reminderRecipients is who hears about an approaching deadline:
//
//   - the Risk Assigner, always — they drive remediation and must submit it
//     for completion approval before the date;
//   - every Action Owner with at least one unfinished plan, who still has
//     steps to complete. An owner whose plans are all COMPLETED is left out:
//     a reminder asks someone to act, and they have nothing left to do. This
//     is deliberately unlike escalation, which tells everyone involved that
//     the date was missed;
//   - the Risk Owner, on the due date only — accountable for the risk, and
//     worth telling on the last day it can still be saved from escalation;
//   - the compliance roles, on every tier, for oversight.
//
// The Management Approver is never reminded; they hear about the risk if it
// escalates.
func (d *Deps) reminderRecipients(ctx context.Context, detail *model.RiskDetail, plans []*model.ActionPlan, tier string) []int {
	ids := []int{detail.AssignerID}

	for _, pl := range plans {
		if pl == nil || pl.ActionOwnerID == nil || pl.Status == completedPlanStatus {
			continue
		}
		ids = append(ids, *pl.ActionOwnerID)
	}

	if tier == model.ReminderDueToday {
		ids = append(ids, detail.OwnerID)
	}

	ids = append(ids, d.complianceOversight(ctx, detail)...)

	// resolveRecipientEmails de-duplicates, so the same person filling two of
	// these roles still gets exactly one email.
	return ids
}

// complianceOversight resolves the Risk Compliance Team and Risk Compliance
// Admin in the risk's own register, in one call.
//
// RISK_VIEW_ALL_RISKS is what those two roles have and no other role does, so
// it selects both without naming either — the same privilege-not-role-name
// rule every other recipient lookup follows. Granting that privilege to a new
// role would therefore also subscribe it to these reminders.
//
// Scoped to the risk's source register: a GLOBAL grant matches every register,
// a register-scoped one only its own. A failure is logged and treated as "no
// oversight recipients" — the Assigner and Action Owners are the reminder's
// primary audience and must not be lost to a failed grant lookup.
func (d *Deps) complianceOversight(ctx context.Context, detail *model.RiskDetail) []int {
	if d.Grants == nil {
		// Local dev: no privilege store, so there are no grants to resolve.
		return nil
	}
	candidates, err := d.Grants.Candidates(ctx, privilege.ViewAllRisks, []int{detail.SourceRegisterID})
	if err != nil {
		slog.Warn("risk reminder: failed to resolve compliance recipients, sending without them",
			"riskId", detail.ID, "registerId", detail.SourceRegisterID, "err", err)
		return nil
	}
	ids := make([]int, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.ID)
	}
	return ids
}

// reminderPeople fills the "Who needs to act" block: the Action Owners who
// still have work, and the Assigner. The Risk Owner and the compliance roles
// receive the email but get no line there — they are being kept informed, not
// tasked, the same split EventEscalated draws.
//
// Built here rather than through peopleForEvent because that helper lists
// EVERY plan owner, which is right for escalation and wrong here: an owner
// whose plans are all done isn't even a recipient.
func (d *Deps) reminderPeople(ctx context.Context, detail *model.RiskDetail, plans []*model.ActionPlan) map[string][]string {
	people := map[string][]string{}

	if p := d.personLabel(ctx, detail.AssignerID); p != "" {
		people[emailer.RoleRiskAssigner] = []string{p}
	}

	seen := map[int]bool{}
	for _, pl := range plans {
		if pl == nil || pl.ActionOwnerID == nil || pl.Status == completedPlanStatus || seen[*pl.ActionOwnerID] {
			continue
		}
		seen[*pl.ActionOwnerID] = true
		if p := d.personLabel(ctx, *pl.ActionOwnerID); p != "" {
			people[emailer.RoleActionOwner] = append(people[emailer.RoleActionOwner], p)
		}
	}

	if len(people) == 0 {
		return nil
	}
	return people
}
