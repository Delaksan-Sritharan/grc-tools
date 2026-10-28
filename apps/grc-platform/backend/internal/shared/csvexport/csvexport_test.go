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

package csvexport

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSafeCell(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "plain": "plain", "=1+1": "'=1+1", "+1": "'+1", "-1": "'-1", "@x": "'@x",
		"\tx": "'\tx", "\rx": "'\rx", "a=b": "a=b",
	} {
		if got := SafeCell(in); got != want {
			t.Errorf("SafeCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDate(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "2026-11-05T00:00:00Z": "2026-11-05", "2026-10-01T08:30:00.123456Z": "2026-10-01",
		"2026-11-05": "2026-11-05", "not a date": "not a date",
	} {
		if got := Date(in); got != want {
			t.Errorf("Date(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeref(t *testing.T) {
	s := "x"
	if Deref(nil) != "" || Deref(&s) != "x" {
		t.Error("Deref should return the value, or empty for nil")
	}
}

func TestBegin(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := Begin(rec, "report.csv", time.Minute)
	_ = cw.Write([]string{"a", "b,c"})
	cw.Flush()

	if rec.Code != 200 {
		t.Errorf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment; filename=report.csv" {
		t.Errorf("Content-Disposition = %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if !strings.HasPrefix(rec.Body.String(), "\xEF\xBB\xBFa,\"b,c\"") {
		t.Errorf("body = %q", rec.Body.String())
	}
}
