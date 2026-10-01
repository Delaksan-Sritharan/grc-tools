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
	"net/http"
	"path"
	"strings"

	"github.com/wso2-open-operations/grc-tools/apps/grc-platform/backend/internal/shared/llm"
)

// Caps bound each job's cost — skipped items are always named back to the
// model rather than silently dropped, so a PASS can never rest on a file the
// model never actually saw.
const (
	maxFiles      = 10
	maxFileBytes  = 5 << 20  // ~5 MB
	maxTotalBytes = 20 << 20 // ~20 MB
)

// supportedExt is the evidence/population upload accept-list, minus
// zip/msg/eml, which are always listed as unsupported.
var supportedExt = map[string]bool{
	"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true,
	"ppt": true, "pptx": true, "csv": true, "txt": true,
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true,
}

// fileRef is one submitted file's identity, independent of whether it came
// from an evidence round or a population round.
type fileRef struct {
	ID   int
	Name string
}

// FileDownloader fetches one file's bytes by ID. Satisfied by
// service.EvidenceService and service.PopulationService's DownloadFile
// methods without any adapter.
type FileDownloader interface {
	DownloadFile(ctx context.Context, fileID int) (data []byte, fileName, contentType string, err error)
}

// buildFileContent fetches and converts files into LLM content blocks, plus
// a manifest text block listing every file's fate (reviewed or why not) —
// the system prompt tells the model never to base a PASS on an unreviewed
// file, so this manifest matters as much as the blocks themselves.
func buildFileContent(ctx context.Context, dl FileDownloader, files []fileRef) ([]llm.Block, string) {
	var blocks []llm.Block
	var manifest strings.Builder
	manifest.WriteString("Files in this submission:\n")
	if len(files) == 0 {
		manifest.WriteString("(none)\n")
	}

	var total int64
	for i, f := range files {
		if i >= maxFiles {
			fmt.Fprintf(&manifest, "- %s: not reviewed (over the %d-file cap for this job)\n", f.Name, maxFiles)
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(f.Name), "."))
		if !supportedExt[ext] {
			fmt.Fprintf(&manifest, "- %s: not reviewed (unsupported format)\n", f.Name)
			continue
		}
		data, _, _, err := dl.DownloadFile(ctx, f.ID)
		if err != nil {
			fmt.Fprintf(&manifest, "- %s: not reviewed (could not be read)\n", f.Name)
			continue
		}
		if int64(len(data)) > maxFileBytes {
			fmt.Fprintf(&manifest, "- %s: not reviewed (exceeds the %dMB per-file cap)\n", f.Name, maxFileBytes>>20)
			continue
		}
		if total+int64(len(data)) > maxTotalBytes {
			fmt.Fprintf(&manifest, "- %s: not reviewed (job's %dMB total cap reached)\n", f.Name, maxTotalBytes>>20)
			continue
		}

		newBlocks, note := blocksForFile(ext, f.Name, data)
		fmt.Fprintf(&manifest, "- %s: %s\n", f.Name, note)
		if len(newBlocks) > 0 {
			blocks = append(blocks, newBlocks...)
			total += int64(len(data))
		}
	}
	return blocks, manifest.String()
}

// blocksForFile converts one file's bytes into content blocks by extension,
// plus a short manifest note.
func blocksForFile(ext, name string, data []byte) (blocks []llm.Block, manifestNote string) {
	switch ext {
	case "pdf":
		// Embedded screenshots on a PDF page are already visible to the model
		// as a native document block — no separate extraction needed.
		return []llm.Block{llm.NewPDFBlock(data)}, "reviewed"
	case "png", "jpg", "jpeg", "gif", "webp":
		mt := http.DetectContentType(data)
		if !supportedImageMediaType(mt) {
			return nil, "not reviewed (image format not supported)"
		}
		return []llm.Block{llm.NewImageBlock(mt, data)}, "reviewed"
	case "xlsx":
		text, err := XLSXToCSV(data)
		if err != nil {
			return nil, "not reviewed (could not parse spreadsheet)"
		}
		return []llm.Block{llm.NewTextBlock("--- " + name + " ---\n" + text)}, "reviewed"
	case "csv", "txt":
		return []llm.Block{llm.NewTextBlock("--- " + name + " ---\n" + string(data))}, "reviewed"
	case "docx", "pptx":
		return imageBlocksAndNote(extractOOXMLImages(data))
	case "doc", "ppt", "xls":
		return imageBlocksAndNote(extractLegacyOLEImages(data))
	default:
		// zip/msg/eml, or anything else not in supportedExt — unreachable in
		// practice since the caller gates on supportedExt first, kept as a
		// safety net.
		return nil, "not reviewed (unsupported format)"
	}
}

func imageBlocksAndNote(imgs []extractedImage) ([]llm.Block, string) {
	if len(imgs) == 0 {
		return nil, "reviewed (no embedded images found)"
	}
	blocks := make([]llm.Block, 0, len(imgs))
	for _, img := range imgs {
		blocks = append(blocks, llm.NewImageBlock(img.MediaType, img.Data))
	}
	return blocks, fmt.Sprintf("reviewed (%d embedded image(s) extracted)", len(imgs))
}
