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

// Package csvexport holds the pieces every CSV download in the platform shares:
// the response headers, a UTF-8 BOM so Excel reads non-ASCII text correctly, and
// the cell hygiene that keeps user-authored text from running as a formula.
package csvexport

import (
	"encoding/csv"
	"mime"
	"net/http"
	"strings"
	"time"
)

// Begin starts a CSV attachment response: it extends the write deadline to
// writeTimeout, sets the headers, writes the 200 status and the BOM, and returns
// the writer. Anything that can still fail must be done before calling it,
// because the status can't change afterwards.
func Begin(w http.ResponseWriter, fileName string, writeTimeout time.Duration) *csv.Writer {
	// Best effort: ResponseWriters that can't extend the deadline keep the server's.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(writeTimeout))

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	return csv.NewWriter(w)
}

// SafeCell neutralises spreadsheet formula injection: a cell that starts with a
// formula trigger is prefixed with a quote so Excel/Sheets treat it as text.
// Titles, descriptions and comments are user-authored, so this matters.
func SafeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// Date trims an RFC3339 timestamp to YYYY-MM-DD, leaving anything that doesn't
// parse (including a value that is already a plain date) as it came.
func Date(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	return s
}

// Deref returns *s, or "" for nil.
func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
