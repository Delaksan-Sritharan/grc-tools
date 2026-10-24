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
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/directory"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
)

// Name enrichment.
//
// A risk names several people, and the data layer only ever returns each
// person's uuid — the platform stores no name or email anywhere anymore, in
// either service. Turning those uuids back into names is this file's job,
// done once per response rather than once per row.
//
// The batching is the point. A page of 50 risks names up to 150 people, and
// resolving them one at a time would be 150 calls to a service on the far side
// of an API gateway. Collecting the uuids first means the directory is asked
// only about the ones its cache does not already hold.
//
// An unresolvable uuid leaves the field blank, not stale — there is no stored
// value left to fall back to. This should be rare in practice: every role a
// risk actually names is only reachable through a picker or resolve flow that
// already required a successful directory lookup (see candidates.go,
// resolve.go), so a blank name here means someone's directory entry changed
// or disappeared after they were assigned, not that resolution was never
// attempted.

// enrichListItems fills each item's owner and assigner names from the identity
// directory, in one batch.
func (d *Deps) enrichListItems(ctx context.Context, items []*model.RiskListItem) {
	if d.Directory == nil || len(items) == 0 {
		return
	}
	uuids := make([]string, 0, len(items)*2)
	for _, it := range items {
		uuids = append(uuids, it.OwnerUUID, it.AssignerUUID)
	}
	people := d.Directory.LookupAll(ctx, uuids)
	if len(people) == 0 {
		return
	}
	for _, it := range items {
		applyName(people, it.OwnerUUID, &it.OwnerName)
		applyName(people, it.AssignerUUID, &it.AssignerName)
	}
}

// enrichDetail fills a single risk's people from the identity directory.
func (d *Deps) enrichDetail(ctx context.Context, detail *model.RiskDetail) {
	if d.Directory == nil || detail == nil {
		return
	}
	people := d.Directory.LookupAll(ctx, []string{
		detail.OwnerUUID, detail.AssignerUUID, detail.ManagementApproverUUID, detail.ComplianceApproverUUID,
	})
	if len(people) == 0 {
		return
	}
	applyName(people, detail.OwnerUUID, &detail.OwnerName)
	applyName(people, detail.AssignerUUID, &detail.AssignerName)
	applyName(people, detail.ManagementApproverUUID, &detail.ManagementApproverName)
	// ComplianceApproverName is *string, not string like the others — nil
	// until a risk actually clears compliance approval, at which point
	// ComplianceApproverUUID is set and this fills in the name to match.
	if detail.ComplianceApproverUUID != "" {
		if p, ok := people[detail.ComplianceApproverUUID]; ok && p.DisplayName != "" {
			name := p.DisplayName
			detail.ComplianceApproverName = &name
		}
	}
}

// enrichDashboard fills the owner names on the dashboard's high-risk table.
//
// Only that table names anybody — every other panel counts or groups rather
// than listing people — so this resolves one role rather than three.
func (d *Deps) enrichDashboard(ctx context.Context, dash *model.DashboardSummary) {
	if d.Directory == nil || dash == nil || len(dash.HighRisks) == 0 {
		return
	}
	uuids := make([]string, 0, len(dash.HighRisks))
	for _, h := range dash.HighRisks {
		uuids = append(uuids, h.OwnerUUID)
	}
	people := d.Directory.LookupAll(ctx, uuids)
	if len(people) == 0 {
		return
	}
	for i := range dash.HighRisks {
		applyName(people, dash.HighRisks[i].OwnerUUID, &dash.HighRisks[i].OwnerName)
	}
}

// enrichAnalytics fills the owner names on the analytics aging-risks table,
// the only part of that payload naming a person.
func (d *Deps) enrichAnalytics(ctx context.Context, sum *model.AnalyticsSummary) {
	if d.Directory == nil || sum == nil || len(sum.AgingRisks) == 0 {
		return
	}
	uuids := make([]string, 0, len(sum.AgingRisks))
	for _, a := range sum.AgingRisks {
		uuids = append(uuids, a.OwnerUUID)
	}
	people := d.Directory.LookupAll(ctx, uuids)
	if len(people) == 0 {
		return
	}
	for i := range sum.AgingRisks {
		applyName(people, sum.AgingRisks[i].OwnerUUID, &sum.AgingRisks[i].OwnerName)
	}
}

// enrichHistory fills each entry's actor email from the identity directory —
// the History tab shows who did what, and the data layer only ever returns
// the actor's uuid — and swaps the user and team ids in assignee diffs for
// names, which is what a reader can use.
func (d *Deps) enrichHistory(ctx context.Context, entries []*model.HistoryEntry) {
	if len(entries) == 0 {
		return
	}
	d.nameHistoryAssignees(ctx, entries)
	if d.Directory == nil {
		return
	}
	uuids := make([]string, 0, len(entries))
	for _, e := range entries {
		uuids = append(uuids, e.CreatedBy)
	}
	people := d.Directory.LookupAll(ctx, uuids)
	if len(people) == 0 {
		return
	}
	for _, e := range entries {
		applyEmail(people, e.CreatedBy, &e.CreatedByEmail)
		applyName(people, e.CreatedBy, &e.CreatedByName)
	}
}

// historyUserFields hold a platform user id in their diff; assignment_team_id
// holds a team id. Everything else in a diff is already readable.
var historyUserFields = map[string]bool{
	"assigner_id":            true,
	"owner_id":               true,
	"management_approver_id": true,
	"action_owner_id":        true,
}

const historyTeamField = "assignment_team_id"

// nameHistoryAssignees rewrites the old and new value of assignee diffs from an
// id to the person's or team's name. The values are stored as JSON strings of
// the id ("21"), so a resolved name goes back as a JSON string too. Anything
// that cannot be resolved keeps its id: the client shows no value for a bare
// number rather than a misleading one, and the entry itself is never dropped.
func (d *Deps) nameHistoryAssignees(ctx context.Context, entries []*model.HistoryEntry) {
	userIDs := map[int]bool{}
	needTeams := false
	for _, e := range entries {
		if e.FieldChanged == nil {
			continue
		}
		switch {
		case historyUserFields[*e.FieldChanged]:
			for _, v := range []*string{e.OldValue, e.NewValue} {
				if id, ok := historyID(v); ok {
					userIDs[id] = true
				}
			}
		case *e.FieldChanged == historyTeamField:
			needTeams = true
		}
	}

	userNames := d.userNamesByID(ctx, userIDs)
	var teamNames map[int]string
	if needTeams && d.Team != nil {
		teams, err := d.Team.List(ctx, model.ListTeamsFilter{IncludeInactive: true})
		if err != nil {
			slog.WarnContext(ctx, "risk history: list teams for names failed", "err", err)
		}
		teamNames = make(map[int]string, len(teams))
		for _, t := range teams {
			teamNames[t.ID] = t.Name
		}
	}

	for _, e := range entries {
		if e.FieldChanged == nil {
			continue
		}
		var names map[int]string
		switch {
		case historyUserFields[*e.FieldChanged]:
			names = userNames
		case *e.FieldChanged == historyTeamField:
			names = teamNames
		default:
			continue
		}
		e.OldValue = historyNamed(e.OldValue, names)
		e.NewValue = historyNamed(e.NewValue, names)
	}
}

// userNamesByID resolves platform user ids to display names: id -> uuid through
// the user table, then uuid -> name through the identity directory. Ids it
// cannot resolve are simply absent from the result.
func (d *Deps) userNamesByID(ctx context.Context, ids map[int]bool) map[int]string {
	if len(ids) == 0 || d.Users == nil || d.Directory == nil {
		return nil
	}
	uuidByID := make(map[int]string, len(ids))
	uuids := make([]string, 0, len(ids))
	for id := range ids {
		u, err := d.Users.GetByID(ctx, id)
		if err != nil || u == nil || u.UUID == "" {
			continue
		}
		uuidByID[id] = u.UUID
		uuids = append(uuids, u.UUID)
	}
	people := d.Directory.LookupAll(ctx, uuids)
	names := make(map[int]string, len(uuidByID))
	for id, uuid := range uuidByID {
		if p, ok := people[uuid]; ok && p.DisplayName != "" {
			names[id] = p.DisplayName
		}
	}
	return names
}

// historyID reads a diff value ("21", a JSON string of an id) as an int.
func historyID(raw *string) (int, bool) {
	if raw == nil {
		return 0, false
	}
	var s string
	if err := json.Unmarshal([]byte(*raw), &s); err != nil {
		return 0, false
	}
	id, err := strconv.Atoi(s)
	return id, err == nil
}

// historyNamed returns raw with its id replaced by names[id], or raw unchanged
// when it is not an id or has no name.
func historyNamed(raw *string, names map[int]string) *string {
	id, ok := historyID(raw)
	if !ok {
		return raw
	}
	name, ok := names[id]
	if !ok {
		return raw
	}
	encoded, err := json.Marshal(name)
	if err != nil {
		return raw
	}
	out := string(encoded)
	return &out
}

// enrichEvidence fills each file's uploader email from the identity
// directory, same pattern as enrichHistory.
func (d *Deps) enrichEvidence(ctx context.Context, evidence []*model.RiskEvidence) {
	if d.Directory == nil || len(evidence) == 0 {
		return
	}
	uuids := make([]string, 0, len(evidence))
	for _, e := range evidence {
		uuids = append(uuids, e.CreatedBy)
	}
	people := d.Directory.LookupAll(ctx, uuids)
	if len(people) == 0 {
		return
	}
	for _, e := range evidence {
		applyEmail(people, e.CreatedBy, &e.CreatedByEmail)
	}
}

// applyEmail writes the directory's email for uuid into dst, leaving dst
// alone when the directory has nothing to say — same fallback shape as
// applyName, so an unresolvable actor keeps CreatedByEmail empty rather than
// a blank-looking placeholder, and the client falls back to the raw uuid.
func applyEmail(people map[string]directory.Person, uuid string, dst *string) {
	if uuid == "" {
		return
	}
	if p, ok := people[uuid]; ok && p.Email != "" {
		*dst = p.Email
	}
}

// applyName writes the directory's name for uuid into dst, leaving dst alone
// (i.e. blank — the data layer never populates it) when the directory has
// nothing to say. The identity directory is the only source of a name now;
// there is no stored value left to fall back to.
func applyName(people map[string]directory.Person, uuid string, dst *string) {
	if uuid == "" {
		return
	}
	if p, ok := people[uuid]; ok && p.DisplayName != "" {
		*dst = p.DisplayName
	}
}
