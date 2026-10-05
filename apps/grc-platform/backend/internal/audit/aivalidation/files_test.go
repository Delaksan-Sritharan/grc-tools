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

package aivalidation

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeDownloader struct{}

func (fakeDownloader) DownloadFile(_ context.Context, fileID int) ([]byte, string, string, error) {
	return []byte(fmt.Sprintf("content %d", fileID)), "", "", nil
}

// Skipped files must not use up the file cap: a reviewable file after
// maxFiles unsupported ones is still reviewed.
func TestBuildFileContent_SkippedFilesDoNotCountTowardCap(t *testing.T) {
	var files []fileRef
	for i := 0; i < maxFiles; i++ {
		files = append(files, fileRef{ID: i, Name: fmt.Sprintf("archive-%d.zip", i)})
	}
	files = append(files, fileRef{ID: 100, Name: "evidence.txt"})

	blocks, manifest := buildFileContent(context.Background(), fakeDownloader{}, files, newJobBudget())

	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if !strings.Contains(manifest, "- \"evidence.txt\": reviewed\n") {
		t.Errorf("evidence.txt not marked reviewed in manifest:\n%s", manifest)
	}
}

// The cap still applies to files actually sent.
func TestBuildFileContent_CapsSentFiles(t *testing.T) {
	var files []fileRef
	for i := 0; i < maxFiles+2; i++ {
		files = append(files, fileRef{ID: i, Name: fmt.Sprintf("note-%d.txt", i)})
	}

	blocks, manifest := buildFileContent(context.Background(), fakeDownloader{}, files, newJobBudget())

	if len(blocks) != maxFiles {
		t.Fatalf("got %d blocks, want %d", len(blocks), maxFiles)
	}
	if got := strings.Count(manifest, "over the"); got != 2 {
		t.Errorf("got %d over-cap entries, want 2:\n%s", got, manifest)
	}
}
