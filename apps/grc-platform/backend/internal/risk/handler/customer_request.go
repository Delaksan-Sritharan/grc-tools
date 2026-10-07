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
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/response"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/auth"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/emailer"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/grant"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

// customerCodePattern mirrors the Compliance Entity's rule for customer codes,
// so a malformed suggestion is caught while the requester is still on the form
// rather than when an admin tries to use it.
var customerCodePattern = regexp.MustCompile(`^[A-Z0-9]{1,12}$`)

const (
	maxCustomerNameLen = 255
	maxCustomerNoteLen = 1000

	customerRequestWindow  = time.Hour
	customerRequestPerUser = 5 // all names combined, so varying the name can't dodge the per-name rule
)

// customerRequestLimiter caps how often one requester can email the platform
// admins: the same customer name once per window, and customerRequestPerUser
// requests of any name per window. In memory, so each backend replica keeps its
// own count (the real cap is up to the replica count times these numbers); that
// still bounds a script or a double-click loop, which is all it is for. A nil
// limiter allows everything (Deps built without RegisterRoutes, i.e. tests).
type customerRequestLimiter struct {
	mu   sync.Mutex
	sent map[string][]customerRequestEntry // requester uuid -> sends in the window
}

type customerRequestEntry struct {
	name string // lowercased customer name
	at   time.Time
}

// allow records the request and returns true, or returns false without
// recording when the requester is over either limit.
func (l *customerRequestLimiter) allow(requester, customerName string, now time.Time) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sent == nil {
		l.sent = make(map[string][]customerRequestEntry)
	}
	name := strings.ToLower(customerName)
	recent := l.sent[requester][:0]
	for _, e := range l.sent[requester] {
		if now.Sub(e.at) < customerRequestWindow {
			recent = append(recent, e)
		}
	}
	l.sent[requester] = recent
	if len(recent) >= customerRequestPerUser {
		return false
	}
	for _, e := range recent {
		if e.name == name {
			return false
		}
	}
	l.sent[requester] = append(recent, customerRequestEntry{name: name, at: now})
	return true
}

// handleRequestCustomer serves POST /api/v1/risks/customer-requests.
//
// A Risk Assigner who cannot find a customer in the Customer Name dropdown
// asks the platform admins (every holder of MANAGE_RISK_HUB) to add it. The
// email goes to them and to the requester together, and nothing is stored:
// the email is the record, and the admin replies to it once the customer
// exists (RISK_MODULE_DESIGN.md §14).
//
// Only someone who could raise a Managed Services risk may ask — anyone else
// would be emailing every platform admin for nothing.
//
// The send is detached, like every other notification, so this answers 202:
// the request was accepted for sending, not delivered. The requester is on
// the email themselves, so a missing copy tells them it did not go out.
func (d *Deps) handleRequestCustomer(w http.ResponseWriter, r *http.Request) {
	requester, ok := requireCallerUUID(w, r)
	if !ok {
		return
	}
	var req model.CustomerRequestPayload
	if err := response.DecodeJSON(w, r, &req); err != nil {
		return
	}
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	req.SuggestedCode = strings.TrimSpace(req.SuggestedCode)
	req.Note = strings.TrimSpace(req.Note)
	switch {
	case req.CustomerName == "":
		response.WriteError(w, http.StatusBadRequest, "customer_name is required")
		return
	case utf8.RuneCountInString(req.CustomerName) > maxCustomerNameLen:
		response.WriteError(w, http.StatusBadRequest, "customer_name is too long")
		return
	case req.SuggestedCode != "" && !customerCodePattern.MatchString(req.SuggestedCode):
		response.WriteError(w, http.StatusBadRequest, "suggested_code must be 1-12 uppercase letters (A-Z) or digits (0-9)")
		return
	case utf8.RuneCountInString(req.Note) > maxCustomerNoteLen:
		response.WriteError(w, http.StatusBadRequest, "note is too long")
		return
	}

	canRaise, err := d.canCreateManagedServicesRisk(r.Context())
	if err != nil {
		response.MapServiceError(r.Context(), w, err, response.ErrMsgInternal)
		return
	}
	if !canRaise {
		response.WriteError(w, http.StatusForbidden, "only someone who can raise Managed Services risks can request a customer")
		return
	}

	if !d.customerRequests.allow(requester, req.CustomerName, time.Now()) {
		response.WriteError(w, http.StatusTooManyRequests,
			"you have already sent this request recently; the platform admins have it")
		return
	}

	d.notifyCustomerRequest(requester, req)
	response.WriteJSONValue(w, http.StatusAccepted, map[string]string{
		"message": "Your request has been sent to the platform admins. You are copied on the email.",
	})
}

// canCreateManagedServicesRisk reports whether the caller holds RISK_CREATE
// in at least one register on the Managed Services template.
func (d *Deps) canCreateManagedServicesRisk(ctx context.Context) (bool, error) {
	registers, err := d.Team.List(ctx, model.ListTeamsFilter{Type: "SOURCE_REGISTER"})
	if err != nil {
		return false, err
	}
	for _, t := range registers {
		if t.RegisterTemplate == model.TemplateManagedServices && auth.HasPrivilegeIn(ctx, privilege.CreateRisk, t.ID) {
			return true, nil
		}
	}
	return false, nil
}

// notifyCustomerRequest emails the platform admins and the requester, detached
// and best-effort like notifyComplianceAdmins.
func (d *Deps) notifyCustomerRequest(requesterUUID string, req model.CustomerRequestPayload) {
	if d.Grants == nil {
		// Local dev: no privilege store, so no admins to resolve.
		slog.Warn("customer request: no grant store configured, not sent", "customer", req.CustomerName)
		return
	}
	go func() {
		defer func() {
			if p := recover(); p != nil {
				slog.Error("customer request: panic", "customer", req.CustomerName, "panic", p)
			}
		}()
		notifySem <- struct{}{}
		defer func() { <-notifySem }()
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()

		// MANAGE_RISK_HUB is SHARED, so it is only ever granted GLOBAL.
		adminIDs, err := grant.CandidateIDs(ctx, d.Grants, privilege.ManageRiskHub)
		if err != nil {
			slog.Warn("customer request: failed to resolve platform admins", "customer", req.CustomerName, "err", err)
			return
		}
		emails := d.resolveRecipientEmails(ctx, "customer-request", 0, adminIDs)
		if len(emails) == 0 {
			slog.Warn("customer request: no platform admin has a deliverable address", "customer", req.CustomerName)
			return
		}
		if _, requesterEmail := d.resolvePerson(ctx, requesterUUID); requesterEmail != "" {
			emails = appendUnique(emails, requesterEmail)
		}
		if err := d.Email.SendCustomerRequest(ctx, emails, emailer.CustomerRequest{
			CustomerName:  req.CustomerName,
			SuggestedCode: req.SuggestedCode,
			Note:          req.Note,
			Requester:     d.describeActor(ctx, requesterUUID),
			AdminURL:      d.FrontendBaseURL + "/admin/risk-hub?tab=customers",
		}); err != nil {
			slog.Warn("customer request: send failed", "customer", req.CustomerName, "err", err)
			return
		}
		slog.Info("customer request sent", "customer", req.CustomerName, "recipients", len(emails))
	}()
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return list
		}
	}
	return append(list, s)
}
