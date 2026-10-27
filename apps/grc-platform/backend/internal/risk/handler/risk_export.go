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
	"encoding/csv"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/response"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/risk/model"
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
	// Same stance as handleListRisks: no privilege gate, visibility is decided
	// entirely by buildListFilter's scoping, and each branch fails closed.
	if _, ok := requireCallerUUID(w, r); !ok {
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

	// Best effort: ResponseWriters that can't extend the deadline keep the server's.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(exportWriteTimeout))

	fileName := "risk-report-" + time.Now().UTC().Format("2006-01-02") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	w.WriteHeader(http.StatusOK)

	// UTF-8 BOM so Excel reads non-ASCII names correctly.
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
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
		csvCell(it.RiskCode),
		csvCell(it.RiskTitle),
		csvCell(it.SourceRegisterName),
		csvCell(it.RiskLevel),
		csvCell(it.WorkflowStatus),
		csvCell(it.RiskType),
		csvCell(derefString(it.TreatmentStrategy)),
		csvCell(it.OwnerName),
		csvCell(it.AssignerName),
		csvCell(exportDate(derefString(it.ImplementationDate))),
		csvCell(exportDate(it.CreatedAt)),
		csvCell(derefString(it.CustomerName)),
		csvCell(strings.Join(it.PlatformNames, "; ")),
		csvCell(strings.Join(it.Environments, "; ")),
		csvCell(derefString(it.RejectionStage)),
		csvCell(derefString(it.RejectionComment)),
	}
}

// csvCell neutralises spreadsheet formula injection: a cell that starts with a
// formula trigger is prefixed with a quote so Excel/Sheets treat it as text.
// Risk titles and rejection comments are user-authored, so this matters.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// exportDate trims an RFC3339 timestamp to YYYY-MM-DD, leaving anything that
// doesn't parse as it came.
func exportDate(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	return s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
