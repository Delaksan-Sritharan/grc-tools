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
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/apierror"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/domain"
	"github.com/wso2-open-operations/grc-tools/entity/compliance-entity/internal/service"
)

// RiskReminderHandler handles /risk/reminders routes.
type RiskReminderHandler struct {
	svc service.RiskReminderService
}

// NewRiskReminderHandler constructs a RiskReminderHandler.
func NewRiskReminderHandler(svc service.RiskReminderService) *RiskReminderHandler {
	return &RiskReminderHandler{svc: svc}
}

// ClaimRiskReminder handles POST /risk/reminders/claim — the due-date
// reminder sweep's atomic de-dup claim. POST-with-body rather than
// GET-with-query because it writes. Losing the race (claimed=false) is a
// normal 200, not an error: it just means another replica got there first.
func (h *RiskReminderHandler) ClaimRiskReminder(w http.ResponseWriter, r *http.Request) {
	var req domain.ClaimRiskReminderRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	claimed, id, err := h.svc.ClaimRiskReminder(r.Context(), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(domain.ClaimRiskReminderResponse{Claimed: claimed, ID: id})
}

// ReleaseRiskReminderClaim handles DELETE /risk/reminders/{id}/claim —
// releases a claim whose email failed, so a later run the same day sends it.
func (h *RiskReminderHandler) ReleaseRiskReminderClaim(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeServiceError(w, r, &apierror.ValidationError{Msg: "id must be a positive integer"})
		return
	}
	if err := h.svc.ReleaseRiskReminderClaim(r.Context(), id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
