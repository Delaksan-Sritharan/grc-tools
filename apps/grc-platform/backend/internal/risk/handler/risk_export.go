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
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/response"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/auth"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/csvexport"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

const (
	// exportPageSize is the rows fetched per page while walking a full export.
	exportPageSize = 200
	// exportMaxRows bounds one export. Hitting it fails the request rather than
	// handing back a file that silently stops short of what was asked for.
	exportMaxRows = 50000
	// exportWriteTimeout replaces the server-wide write deadline for one export,
	// which can legitimately take longer than an ordinary request.
	exportWriteTimeout = 5 * time.Minute
)

// riskExportHeader is the CSV header row; riskExportRecord emits the same
// columns in the same order.
var riskExportHeader = []string{
	"Risk Code", "Title", "Register", "Residual Level", "Status", "Risk Type",
	"Treatment Strategy", "Owner", "Assigner", "Due Date", "Submitted Date",
	"Customer", "Platforms", "Environments", "Rejection Stage", "Rejection Comment",
}

// handleExportRisks serves GET /api/v1/risks/export.
//
// It takes the same filter query params as handleListRisks, applies the same
// caller scoping, and returns every matching risk as CSV instead of one page.
// limit and offset are ignored.
func (d *Deps) handleExportRisks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireCallerUUID(w, r); !ok {
		return
	}
	// Unlike the list, the export is limited to Compliance: RISK_VIEW_ALL_RISKS is
	// held only by grc-platform-risk-compliance-admin and
	// grc-platform-risk-compliance-team among the seeded roles. A Compliance Team
	// grant can be scoped to specific registers, so this check is unscoped (it
	// passes for a holder at any scope) and the rows are then narrowed to the
	// caller's own scope by buildListFilter, exactly as in the list — the gate
	// decides who may export, never which risks they see. Each scoping branch
	// still fails closed.
	if !auth.RequirePrivilege(r.Context(), w, privilege.ViewAllRisks) {
		return
	}
	filter, empty, ok := d.buildListFilter(w, r)
	if !ok {
		return
	}
	filter.Limit = exportPageSize
	filter.Offset = 0

	// The first page is fetched before anything is written so a failure here can
	// still be a proper error response. Later failures can only abort the stream.
	var page *model.RiskListPage
	if empty {
		page = &model.RiskListPage{Items: []*model.RiskListItem{}}
	} else {
		var err error
		if page, err = d.Risk.List(r.Context(), filter); err != nil {
			response.MapServiceError(r.Context(), w, err, response.ErrMsgInternal)
			return
		}
		if page.Total > exportMaxRows {
			response.WriteError(w, http.StatusUnprocessableEntity,
				"too many risks to export at once; narrow the filters and try again")
			return
		}
	}

	fileName := "risk-report-" + time.Now().UTC().Format("2006-01-02") + ".csv"
	cw := csvexport.Begin(w, fileName, exportWriteTimeout)
	_ = cw.Write(riskExportHeader)

	flusher, _ := w.(http.Flusher)
	for {
		d.enrichListItems(r.Context(), page.Items)
		for _, it := range page.Items {
			_ = cw.Write(riskExportRecord(it))
		}
		cw.Flush()
		if flusher != nil {
			flusher.Flush()
		}

		filter.Offset += len(page.Items)
		if len(page.Items) == 0 || filter.Offset >= page.Total {
			return
		}
		var err error
		if page, err = d.Risk.List(r.Context(), filter); err != nil {
			// Headers are gone; all that's left is to cut the stream short and
			// say so in the log. The client sees a truncated download.
			slog.ErrorContext(r.Context(), "risk export aborted mid-stream", "offset", filter.Offset, "error", err)
			return
		}
	}
}

// riskExportRecord renders one risk as a CSV row.
func riskExportRecord(it *model.RiskListItem) []string {
	return []string{
		csvexport.SafeCell(it.RiskCode),
		csvexport.SafeCell(it.RiskTitle),
		csvexport.SafeCell(it.SourceRegisterName),
		csvexport.SafeCell(it.RiskLevel),
		csvexport.SafeCell(it.WorkflowStatus),
		csvexport.SafeCell(it.RiskType),
		csvexport.SafeCell(csvexport.Deref(it.TreatmentStrategy)),
		csvexport.SafeCell(it.OwnerName),
		csvexport.SafeCell(it.AssignerName),
		csvexport.SafeCell(csvexport.Date(csvexport.Deref(it.ImplementationDate))),
		csvexport.SafeCell(csvexport.Date(it.CreatedAt)),
		csvexport.SafeCell(csvexport.Deref(it.CustomerName)),
		csvexport.SafeCell(strings.Join(it.PlatformNames, "; ")),
		csvexport.SafeCell(strings.Join(it.Environments, "; ")),
		csvexport.SafeCell(csvexport.Deref(it.RejectionStage)),
		csvexport.SafeCell(csvexport.Deref(it.RejectionComment)),
	}
}
