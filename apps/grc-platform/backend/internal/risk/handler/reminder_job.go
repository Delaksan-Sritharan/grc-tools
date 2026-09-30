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
	"sync/atomic"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/response"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/auth"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

// reminderJobHandler exposes a manual trigger for the daily due-date reminder
// sweep (internal/risk/job.ReminderJob), alongside escalationJobHandler's for
// the escalation sweep. It is the only way to run the reminder sweep on
// demand, and the way QA exercises a tier: set a test risk's implementation
// date that many days out and fire this.
type reminderJobHandler struct {
	// trigger runs the sweep's full pass. A plain function (not a
	// job.ReminderJob field) so this package never imports internal/risk/job,
	// which would import back into handler and cycle. Nil (job wiring not
	// configured) answers 503.
	trigger func(ctx context.Context) error
	// running turns a concurrent second trigger into a synchronous 409
	// instead of a 202 followed by a run that silently fails — same reasoning
	// as escalationJobHandler.running.
	running atomic.Bool
}

// run handles POST /api/v1/risks/reminders/run.
//
// Detached in a goroutine rather than run inline, for the same reason as the
// escalation trigger: the server's WriteTimeout is 30s and a sweep may take up
// to 30 minutes (job.runTimeout), so an inline run would fail the response or
// be cut off mid-sweep.
//
// Re-running on the same day is safe by design — every reminder already sent
// is claimed in risk_reminder, so a second pass re-sends nothing and only
// retries sends that failed.
func (h *reminderJobHandler) run(w http.ResponseWriter, r *http.Request) {
	// ManageRiskHub, not a register-scoped privilege: this sweep emails across
	// EVERY register, so it is gated exactly as the escalation trigger is.
	if !auth.RequirePrivilege(r.Context(), w, privilege.ManageRiskHub) {
		return
	}
	if h.trigger == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "reminder job is not configured")
		return
	}
	if !h.running.CompareAndSwap(false, true) {
		response.WriteError(w, http.StatusConflict, "reminder job is already running")
		return
	}
	go func() { // #nosec G118 -- deliberately detached from r.Context(): it would cancel this sweep the instant the handler returns 202, well before the up-to-30min run finishes
		defer h.running.Store(false)
		defer func() {
			if p := recover(); p != nil {
				slog.Error("risk reminder job: manual trigger panic", "panic", p)
			}
		}()
		if err := h.trigger(context.Background()); err != nil {
			slog.Error("risk reminder job: manual trigger failed", "err", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}
