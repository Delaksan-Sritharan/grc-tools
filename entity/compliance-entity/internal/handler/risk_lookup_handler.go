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

// RiskLookupHandler handles the routes of one register-template lookup:
// /risk/platforms, /risk/customers, /risk/products or /risk/deployment-types.
type RiskLookupHandler struct{ svc service.RiskLookupService }

// NewRiskLookupHandler constructs a RiskLookupHandler.
func NewRiskLookupHandler(svc service.RiskLookupService) *RiskLookupHandler {
	return &RiskLookupHandler{svc: svc}
}

// ListRiskLookups handles GET /risk/{lookup}?status=ACTIVE|INACTIVE.
// Without status every value is returned: the Admin Console lists inactive
// values too, while pickers ask for ACTIVE only.
func (h *RiskLookupHandler) ListRiskLookups(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ListRiskLookups(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// CreateRiskLookup handles POST /risk/{lookup}.
func (h *RiskLookupHandler) CreateRiskLookup(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateRiskLookupRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	v, err := h.svc.CreateRiskLookup(r.Context(), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(v)
}

// UpdateRiskLookup handles PATCH /risk/{lookup}/{id}.
func (h *RiskLookupHandler) UpdateRiskLookup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, r, &apierror.ValidationError{Msg: "id must be a positive integer"})
		return
	}
	var req domain.UpdateRiskLookupRequest
	if !decodeRequest(w, r, &req) {
		return
	}
	v, err := h.svc.UpdateRiskLookup(r.Context(), id, req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// DeleteRiskLookup handles DELETE /risk/{lookup}/{id}.
func (h *RiskLookupHandler) DeleteRiskLookup(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeServiceError(w, r, &apierror.ValidationError{Msg: "id must be a positive integer"})
		return
	}
	if err := h.svc.DeleteRiskLookup(r.Context(), id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
