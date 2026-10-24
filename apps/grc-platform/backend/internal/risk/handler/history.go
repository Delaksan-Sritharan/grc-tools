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
	"log/slog"
	"net/http"
	"strconv"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/response"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/auth"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

// recordHistory appends one entry to a risk's history.
//
// Best-effort by design, matching the Audit Hub's recordEvidenceTrail: the
// action being recorded has already succeeded and been committed, so a failure
// to write history must never surface to the caller or undo their work. It is
// logged loudly instead, because a silently short history is the failure mode
// that matters here.
//
// Synchronous rather than detached: this is a single call to the Compliance
// Entity, which the handler is already talking to — unlike the email path,
// there is no cold-starting third party to wait out.
func (d *Deps) recordHistory(ctx context.Context, riskID int, by string, req model.RecordHistoryRequest) {
	if d.History == nil {
		return
	}
	if err := d.History.Record(ctx, riskID, req, by); err != nil {
		slog.WarnContext(ctx, "risk history: record failed",
			"riskId", riskID, "action", req.Action, "err", err)
	}
}

// recordEvent is recordHistory for a workflow event — the common case, where
// there is no field diff and everything interesting is in the details.
func (d *Deps) recordEvent(ctx context.Context, riskID int, by, action string, details model.HistoryDetails) {
	d.recordHistory(ctx, riskID, by, model.RecordHistoryRequest{Action: action, Details: &details})
}

// handleListRiskHistory serves GET /api/v1/risks/{id}/history — the full
// chronological record behind the drawer's History tab.
//
// Visible to anyone who can view the risk, scoped the same way every other
// risk-detail endpoint is: history is a view of actions already visible on the
// risk, so it needs no narrower rule of its own.
func (d *Deps) handleListRiskHistory(w http.ResponseWriter, r *http.Request) {
	// Unscoped on purpose: this gates only whether the caller may read risks at
	// all. WHICH risks they may read is decided by riskVisibleToCaller / the
	// list scoping, not by this privilege.
	if !auth.RequirePrivilege(r.Context(), w, privilege.ViewRisks) {
		return
	}
	riskID, ok := parseRiskID(w, r)
	if !ok {
		return
	}
	visible, err := d.riskVisibleToCaller(r.Context(), riskID)
	if err != nil {
		response.MapServiceError(r.Context(), w, err, response.ErrMsgInternal)
		return
	}
	if !visible {
		response.WriteError(w, http.StatusNotFound, response.ErrMsgNotFound)
		return
	}
	entries, err := d.History.List(r.Context(), riskID)
	if err != nil {
		response.MapServiceError(r.Context(), w, err, response.ErrMsgInternal)
		return
	}
	d.enrichHistory(r.Context(), entries)
	response.WriteJSONValue(w, http.StatusOK, entries)
}

// handleAuditTrail serves GET /api/v1/risks/audit-trail — history across every
// risk the caller can see, behind Risk Hub's Analytics > Audit Trail tab.
//
// Limited to risk owners and the compliance team (canViewAuditTrail), then
// scoped exactly as
// handleListRisks scopes the register, so the trail never shows a change on a
// risk the register would hide. Every branch fails closed: a caller who cannot
// be scoped gets an empty page, never an unscoped one.
func (d *Deps) handleAuditTrail(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireCallerUUID(w, r); !ok {
		return
	}
	if !canViewAuditTrail(r.Context()) {
		response.WriteError(w, http.StatusForbidden, response.ErrMsgForbidden)
		return
	}
	q := r.URL.Query()
	filter := model.AuditTrailFilter{
		From:      q.Get("from"),
		To:        q.Get("to"),
		Actions:   splitCSV(q.Get("action")),
		RiskQuery: q.Get("search"),
		Limit:     50,
	}
	// Max matches the entity's page cap; a larger request would be silently
	// shortened there, leaving the client's paging arithmetic wrong.
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			filter.Limit = v
		}
	}
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			filter.Offset = v
		}
	}
	empty := func() {
		response.WriteJSONValue(w, http.StatusOK, model.AuditTrailPage{
			Items: []*model.AuditTrailEntry{}, Offset: filter.Offset, Limit: filter.Limit,
		})
	}
	// Taken from the caller, never the query string: it widens visibility.
	if user := auth.FromContext(r.Context()); user != nil {
		filter.EscalationLeadUUID = user.Subject
	}
	if holdsNoGrants(r.Context()) {
		callerUUID, _ := requireCallerUUID(w, r)
		caller, err := d.Users.GetByUUID(r.Context(), callerUUID)
		if err != nil {
			response.MapServiceError(r.Context(), w, err, response.ErrMsgInternal)
			return
		}
		if caller == nil {
			empty()
			return
		}
		filter.ActionOwnerID = &caller.ID
	}
	if isTeamScopedOnly(r.Context()) {
		teamIDs := callerGrants(r.Context()).RiskScopeIDs()
		if len(teamIDs) == 0 {
			empty()
			return
		}
		filter.ScopeSourceRegisterIDs = teamIDs
		filter.ScopeAssignmentTeamIDs = teamIDs
	}
	page, err := d.History.Search(r.Context(), filter)
	if err != nil {
		response.MapServiceError(r.Context(), w, err, response.ErrMsgInternal)
		return
	}
	entries := make([]*model.HistoryEntry, len(page.Items))
	for i := range page.Items {
		entries[i] = &page.Items[i].HistoryEntry
	}
	d.enrichHistory(r.Context(), entries)
	response.WriteJSONValue(w, http.StatusOK, page)
}

// Roles that may see the cross-risk audit trail. Named here, not behind a
// privilege, because the privileges the audit trail would otherwise use (view
// analytics) are held by every Risk Hub role, and narrowing them would need a
// change to the seeded role->privilege data.
const (
	roleRiskOwner          = "grc-platform-risk-owner"
	roleRiskComplianceTeam = "grc-platform-risk-compliance-team"
)

// canViewAuditTrail reports whether the caller may open Risk Hub > Audit Trail:
// a risk owner or a member of the compliance team, or anyone in local-dev
// allow-all mode. This is only the front door — which risks the page then lists
// is still decided by the caller's grants (see handleAuditTrail).
func canViewAuditTrail(ctx context.Context) bool {
	if auth.AllowAll(ctx) {
		return true
	}
	return callerGrants(ctx).HasRole(roleRiskOwner, roleRiskComplianceTeam)
}
