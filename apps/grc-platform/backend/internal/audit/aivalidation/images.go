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
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/richardlehane/mscfb"
)

// extractedImage is one embedded picture pulled out of a docx/doc/ppt/pptx/xls
// container, ready to become an llm.Block.
type extractedImage struct {
	Name      string
	MediaType string // image/png | image/jpeg | image/gif | image/webp
	Data      []byte
}

// supportedImageMediaType reports whether typ is one of the image types the
// Anthropic API accepts as a native image block — vector/legacy formats
// (EMF, WMF, TIFF, BMP) are not, and are dropped rather than sent unreadable.
func supportedImageMediaType(typ string) bool {
	switch typ {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// extractOOXMLImages pulls embedded pictures out of a docx or pptx file —
// both are zip archives with media under word/media/ or ppt/media/. Screenshots
// are routinely embedded this way, and unlike xlsx there is no cell data to
// also extract, only the pictures.
func extractOOXMLImages(data []byte) []extractedImage {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	var out []extractedImage
	for _, f := range zr.File {
		if !strings.Contains(f.Name, "/media/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rc, 20<<20))
		rc.Close()
		if err != nil {
			continue
		}
		mediaType := http.DetectContentType(b)
		if !supportedImageMediaType(mediaType) {
			continue
		}
		out = append(out, extractedImage{Name: path.Base(f.Name), MediaType: mediaType, Data: b})
	}
	return out
}

// extractLegacyOLEImages pulls embedded pictures out of a legacy binary
// doc/ppt/xls (OLE Compound File Binary Format) file. There is no lifted
// parser for this container's picture records (MS-ODRAW), so this walks
// every stream and carves out images by file-format signature instead of
// parsing the Escher drawing structure — a pragmatic best-effort approach:
// most embedded screenshots in these legacy formats sit as a near-verbatim
// image file inside a stream, wrapped in a small vendor header. A screenshot
// this misses is reported as an unreviewed file by the caller, same as any
// other file the model can't read — never silently treated as compliant.
func extractLegacyOLEImages(data []byte) []extractedImage {
	r, err := mscfb.New(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	var out []extractedImage
	for range 512 { // hard cap: a compound file can hold many streams
		entry, err := r.Next()
		if err != nil {
			break
		}
		if entry.Size <= 0 || entry.Size > 20<<20 {
			continue
		}
		buf := make([]byte, entry.Size)
		n, _ := io.ReadFull(r, buf)
		buf = buf[:n]
		for _, img := range carveImages(buf) {
			img.Name = entry.Name
			out = append(out, img)
			if len(out) >= 20 {
				return out
			}
		}
	}
	return out
}

// carveImages scans b for embedded JPEG/PNG/GIF file signatures and returns
// each one as a separate image. See extractLegacyOLEImages for why this
// signature-carving approach is used instead of a full MS-ODRAW parse.
func carveImages(b []byte) []extractedImage {
	var out []extractedImage
	out = append(out, carveBySignature(b, []byte{0xFF, 0xD8, 0xFF}, "image/jpeg", jpegLen)...)
	out = append(out, carveBySignature(b, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png", pngLen)...)
	out = append(out, carveBySignature(b, []byte("GIF89a"), "image/gif", gifLen)...)
	out = append(out, carveBySignature(b, []byte("GIF87a"), "image/gif", gifLen)...)
	return out
}

// carveBySignature finds every non-overlapping occurrence of start in b and
// extracts the image beginning there. length reports how many bytes that
// image spans, or -1 when what follows the signature isn't a complete image —
// such a match is skipped rather than carved short.
func carveBySignature(b, start []byte, mediaType string, length func([]byte) int) []extractedImage {
	var out []extractedImage
	pos := 0
	for {
		i := bytes.Index(b[pos:], start)
		if i < 0 {
			break
		}
		from := pos + i
		n := length(b[from:])
		if n < 0 {
			pos = from + len(start)
			continue
		}
		out = append(out, extractedImage{MediaType: mediaType, Data: b[from : from+n]})
		pos = from + n
		if len(out) >= 20 {
			break
		}
	}
	return out
}

// pngLen ends a PNG at its IEND chunk tag plus the 4-byte CRC that follows.
func pngLen(b []byte) int {
	i := bytes.Index(b, []byte("IEND"))
	if i < 0 || i+8 > len(b) {
		return -1
	}
	return i + 8
}

// jpegLen walks a JPEG's marker segments to its real EOI. Searching for the
// first FF D9 instead would stop at an embedded EXIF thumbnail's EOI.
func jpegLen(b []byte) int {
	i := 2 // past SOI
	for i+2 <= len(b) {
		if b[i] != 0xFF {
			return -1
		}
		marker := b[i+1]
		switch {
		case marker == 0xFF: // fill byte
			i++
			continue
		case marker == 0xD9: // EOI
			return i + 2
		case marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7): // no payload
			i += 2
			continue
		}
		if i+4 > len(b) {
			return -1
		}
		i += 2 + (int(b[i+2])<<8 | int(b[i+3]))
		if marker != 0xDA { // not SOS: no entropy-coded data follows
			continue
		}
		// Entropy-coded data runs to the next real marker; FF 00 is a
		// stuffed data byte and FF D0-D7 are restart markers inside it.
		for {
			if i+2 > len(b) {
				return -1
			}
			if b[i] == 0xFF && b[i+1] != 0x00 && !(b[i+1] >= 0xD0 && b[i+1] <= 0xD7) {
				break
			}
			i++
		}
	}
	return -1
}

// gifLen walks a GIF's blocks to its trailer. The trailer byte (0x3B) also
// occurs freely inside colour tables and pixel data, so it can't be searched for.
func gifLen(b []byte) int {
	// colorTable is the size of a colour table described by a packed-flags byte.
	colorTable := func(flags byte) int {
		if flags&0x80 == 0 {
			return 0
		}
		return 3 << ((flags & 0x07) + 1)
	}
	// subBlocks skips a run of length-prefixed data sub-blocks through its
	// zero-length terminator.
	subBlocks := func(i int) int {
		for i < len(b) {
			n := int(b[i])
			i += 1 + n
			if n == 0 {
				return i
			}
		}
		return -1
	}

	if len(b) < 13 {
		return -1
	}
	i := 13 + colorTable(b[10]) // header + logical screen descriptor
	for i >= 0 && i < len(b) {
		switch b[i] {
		case 0x3B: // trailer
			return i + 1
		case 0x21: // extension: label byte, then sub-blocks
			i = subBlocks(i + 2)
		case 0x2C: // image descriptor, local colour table, LZW code size, data
			if i+10 > len(b) {
				return -1
			}
			i = subBlocks(i + 10 + colorTable(b[i+9]) + 1)
		default:
			return -1
		}
	}
	return -1
}
