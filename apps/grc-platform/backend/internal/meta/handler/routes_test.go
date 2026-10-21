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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/middleware"
	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/privilege"
)

const testGuideURL = "https://example.com/guide"

func TestUserGuideURLAccess(t *testing.T) {
	user := &middleware.UserInfo{Subject: "sub", Email: "u@example.com"}

	tests := []struct {
		name string
		ctx  func(context.Context) context.Context
		want string
	}{
		{
			name: "allow-all dev mode (authenticated, nil privilege map)",
			ctx:  func(c context.Context) context.Context { return middleware.WithUserInfo(c, user) },
			want: testGuideURL,
		},
		{
			name: "caller with no grants (empty privilege map)",
			ctx: func(c context.Context) context.Context {
				return privilege.WithContext(middleware.WithUserInfo(c, user), map[string]bool{})
			},
			want: "",
		},
		{
			name: "caller whose privileges are all false",
			ctx: func(c context.Context) context.Context {
				return privilege.WithContext(middleware.WithUserInfo(c, user), map[string]bool{"risk:view": false})
			},
			want: "",
		},
		{
			name: "caller with at least one privilege",
			ctx: func(c context.Context) context.Context {
				return privilege.WithContext(middleware.WithUserInfo(c, user), map[string]bool{"risk:view": true})
			},
			want: testGuideURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			RegisterRoutes(mux, Deps{UserGuideURL: testGuideURL})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/meta/user-guide-url", nil)
			req = req.WithContext(tt.ctx(req.Context()))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got userGuideURLResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode body %q: %v", rec.Body.String(), err)
			}
			if got.URL != tt.want {
				t.Errorf("url = %q, want %q", got.URL, tt.want)
			}
		})
	}
}
