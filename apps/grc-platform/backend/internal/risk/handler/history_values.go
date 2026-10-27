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

import "encoding/json"

// jsonString encodes s the way the change log stores a diff value: as a JSON
// string. The entity rejects an old/new value that is not valid JSON, so a bare
// status or file name would be refused and, being best-effort, silently lost.
func jsonString(s string) *string {
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	out := string(encoded)
	return &out
}

// jsonStringPtr is jsonString for an optional value, preserving nil.
func jsonStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	return jsonString(*s)
}
