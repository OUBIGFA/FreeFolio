package app

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/gabriel-vasile/mimetype"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	oneBotMaxAttachments   = 20
	oneBotMaxFileBytes     = 20 << 20
	oneBotMaxTextBytes     = 2 << 20
	oneBotMaxOfficeBytes   = 4 << 20
	oneBotMaxOfficeEntries = 500
	oneBotThumbSize        = 48
)

type oneBotAttachmentItem struct {
	Part     agent.ContentPart
	SpaceID  string
	RelPath  string
	FullPath string
	Kind     string // "doc", "image", "pdf", "office", "text", "audio", "video"
	Editable bool
	Thumb    *ui.Bitmap
}

// oneBotAttachmentBadge is the lightweight attachment metadata stored with a
// conversation turn; Thumb stays memory-only so saved history stays compact.
type oneBotAttachmentBadge struct {
	Name    string     `json:"name"`
	Kind    string     `json:"kind,omitempty"`
	MIME    string     `json:"mime,omitempty"`
	SpaceID string     `json:"spaceId,omitempty"`
	RelPath string     `json:"relPath,omitempty"`
	Thumb   *ui.Bitmap `json:"-"`
}

func (item oneBotAttachmentItem) badge() oneBotAttachmentBadge {
	return oneBotAttachmentBadge{
		Name:    item.Part.Name,
		Kind:    item.Kind,
		MIME:    item.Part.MIME,
		SpaceID: item.SpaceID,
		RelPath: item.RelPath,
		Thumb:   item.Thumb,
	}
}

type oneBotSpaceSnapshot struct {
	ID       string
	Root     string
	External bool
	Live     map[string]string
	ReadOnly map[string]bool
}

func (ob *qaState) oneBotSpaceSnapshotFor(sp *space.Space) *oneBotSpaceSnapshot {
	if sp == nil {
		return nil
	}
	snap := &oneBotSpaceSnapshot{
		ID:       sp.ID,
		Root:     sp.Root,
		External: sp.IsExternal(),
		Live:     map[string]string{},
		ReadOnly: map[string]bool{},
	}
	for _, d := range ob.host.residentDocuments() {
		if d.space == nil || d.space.ID != sp.ID || d.loadErr != nil {
			continue
		}
		snap.ReadOnly[d.rel] = d.ed.ReadOnly
		if page := d.qaPage(); page != nil {
			snap.Live[d.rel] = strings.Join(page.Lines, "\n")
		}
	}
	return snap
}

func oneBotAttachment(filePath string) (agent.ContentPart, error) {
	item, err := buildOneBotAttachment(filePath, nil)
	return item.Part, err
}

func canonicalAttachmentPath(filePath string) string {
	clean := filepath.Clean(filePath)
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		return real
	}
	return clean
}

func spaceRelForDiskPath(snap *oneBotSpaceSnapshot, canonPath string) (string, bool) {
	if snap == nil || snap.External || snap.Root == "" {
		return "", false
	}
	root := canonicalAttachmentPath(snap.Root)
	rel, err := filepath.Rel(root, canonPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func isEditableSpaceDoc(rel string, readOnly bool) bool {
	if rel == "" || readOnly {
		return false
	}
	return vault.IsMarkdown(rel) || strings.EqualFold(path.Ext(rel), ".arcmd")
}

func formatAttachedFileText(displayName, spacePath string, editable bool, content string) string {
	var b strings.Builder
	attr := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	fmt.Fprintf(&b, `<attached_file name="%s"`, attr.Replace(displayName))
	if spacePath != "" {
		fmt.Fprintf(&b, ` space_path="%s"`, attr.Replace(spacePath))
		if editable {
			b.WriteString(` editable="true"`)
		}
	}
	b.WriteString(">\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("</attached_file>")
	return b.String()
}

func baseMediaType(m *mimetype.MIME) string {
	if m == nil {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(m.String())
	if err != nil {
		return strings.TrimSpace(strings.Split(m.String(), ";")[0])
	}
	return mediaType
}

func isTextDetectedMIME(detected *mimetype.MIME, baseMIME string) bool {
	if detected != nil && detected.Is("text/plain") {
		return true
	}
	if strings.HasPrefix(baseMIME, "text/") {
		return true
	}
	switch baseMIME {
	case "application/json", "application/xml", "application/javascript",
		"application/x-javascript", "image/svg+xml", "application/x-yaml",
		"application/toml", "application/x-sh":
		return true
	}
	return false
}

func buildOneBotAttachment(filePath string, snap *oneBotSpaceSnapshot) (oneBotAttachmentItem, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return oneBotAttachmentItem{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > oneBotMaxFileBytes {
		return oneBotAttachmentItem{}, fmt.Errorf("%s: attachment must be a regular file smaller than 20 MB", filepath.Base(filePath))
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return oneBotAttachmentItem{}, err
	}
	canon := canonicalAttachmentPath(filePath)
	name := filepath.Base(filePath)
	ext := strings.ToLower(filepath.Ext(name))
	relPath, inSpace := spaceRelForDiskPath(snap, canon)
	if !inSpace || !vault.IsText(relPath) {
		relPath = ""
	}
	spaceID := ""
	editable := false
	if relPath != "" && snap != nil {
		spaceID = snap.ID
		editable = isEditableSpaceDoc(relPath, snap.ReadOnly[relPath])
	}
	return parseOneBotAttachmentData(name, ext, canon, spaceID, relPath, editable, data, snap)
}

func parseOneBotAttachmentData(name, ext, canon, spaceID, relPath string, editable bool, data []byte, snap *oneBotSpaceSnapshot) (oneBotAttachmentItem, error) {
	item := oneBotAttachmentItem{
		SpaceID:  spaceID,
		RelPath:  relPath,
		FullPath: canon,
		Editable: editable,
	}
	part := agent.ContentPart{Name: name}

	// Live or native FreeFolio documents take precedence so .arcmd/.excalidraw
	// decode cleanly rather than passing raw JSON to the model.
	if vault.IsText(name) {
		var docText string
		if snap != nil && relPath != "" {
			if live, ok := snap.Live[relPath]; ok {
				docText = live
			}
		}
		if docText == "" {
			if len(data) > oneBotMaxTextBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
				return item, fmt.Errorf("%s: unsupported attachment format", name)
			}
			if vault.IsWhiteboard(name) || ext == ".arcmd" {
				page, err := QAPage(name, string(data))
				if err != nil {
					return item, fmt.Errorf("%s: %w", name, err)
				}
				docText = strings.Join(page.Lines, "\n")
			} else {
				docText = string(data)
			}
		}
		part.Type = "text"
		part.MIME = "text/markdown"
		part.Text = formatAttachedFileText(name, relPath, editable, docText)
		item.Part = part
		item.Kind = "doc"
		return item, nil
	}

	// OOXML Office documents (.docx, .xlsx, .pptx).
	if ext == ".docx" || ext == ".xlsx" || ext == ".pptx" {
		extracted, err := extractOOXMLText(ext, data)
		if err != nil {
			return item, fmt.Errorf("%s: %w", name, err)
		}
		part.Type = "text"
		part.MIME = "text/plain"
		part.Text = formatAttachedFileText(name, "", false, extracted)
		item.Part = part
		item.Kind = "office"
		return item, nil
	}

	detected := mimetype.Detect(data)
	detectedMIME := baseMediaType(detected)
	extMIME := baseMediaTypeString(mime.TypeByExtension(ext))

	// Standard web/model-ready raster images.
	if isDirectImageType(ext, detectedMIME) {
		mimeType := detectedMIME
		if !isDirectImageMIME(mimeType) {
			mimeType = extMIME
		}
		if mimeType == "" {
			mimeType = imageMIMEFromExt(ext)
		}
		if mimeType == "" {
			return item, fmt.Errorf("%s: unknown media type", name)
		}
		part.Type = "image"
		part.MIME = mimeType
		part.Data = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
		item.Part = part
		item.Kind = "image"
		if thumb, err := ui.DecodeBitmapScaled(data, oneBotThumbSize); err == nil {
			item.Thumb = thumb
		}
		return item, nil
	}

	// BMP and TIFF images converted to PNG for broad model compatibility.
	if isConvertibleImageType(ext, detectedMIME) {
		pngBytes, err := convertImageToPNG(ext, detectedMIME, data)
		if err != nil {
			return item, fmt.Errorf("%s: %w", name, err)
		}
		part.Type = "image"
		part.MIME = "image/png"
		part.Data = "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
		item.Part = part
		item.Kind = "image"
		if thumb, err := ui.DecodeBitmapScaled(pngBytes, oneBotThumbSize); err == nil {
			item.Thumb = thumb
		}
		return item, nil
	}

	// PDF files.
	if ext == ".pdf" || detectedMIME == "application/pdf" {
		part.Type = "file"
		part.MIME = "application/pdf"
		part.Data = "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(data)
		item.Part = part
		item.Kind = "pdf"
		return item, nil
	}

	// Audio & video files.
	if kind, mediaMIME := mediaAttachmentKind(ext, detectedMIME, extMIME); kind != "" {
		if mediaMIME == "" {
			return item, fmt.Errorf("%s: unknown media type", name)
		}
		part.Type = kind
		part.MIME = mediaMIME
		part.Data = "data:" + mediaMIME + ";base64," + base64.StdEncoding.EncodeToString(data)
		item.Part = part
		item.Kind = kind
		return item, nil
	}

	// Plain text, code, and structured text formats.
	if len(data) > oneBotMaxTextBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return item, fmt.Errorf("%s: unsupported attachment format", name)
	}
	if detectedMIME != "" && detectedMIME != "application/octet-stream" && !isTextDetectedMIME(detected, detectedMIME) {
		return item, fmt.Errorf("%s: unsupported attachment format", name)
	}
	textMIME := extMIME
	if textMIME == "" {
		textMIME = detectedMIME
	}
	if textMIME == "" {
		textMIME = "text/plain"
	}
	part.Type = "text"
	part.MIME = textMIME
	part.Text = formatAttachedFileText(name, "", false, string(data))
	item.Part = part
	item.Kind = "text"
	return item, nil
}

func baseMediaTypeString(s string) string {
	if s == "" {
		return ""
	}
	mt, _, err := mime.ParseMediaType(s)
	if err != nil {
		return strings.TrimSpace(strings.Split(s, ";")[0])
	}
	return mt
}

func isDirectImageMIME(m string) bool {
	switch m {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

func imageMIMEFromExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}
	return ""
}

func isDirectImageType(ext, detectedMIME string) bool {
	return imageMIMEFromExt(ext) != "" || isDirectImageMIME(detectedMIME)
}

func isConvertibleImageType(ext, detectedMIME string) bool {
	switch ext {
	case ".bmp", ".tif", ".tiff":
		return true
	}
	switch detectedMIME {
	case "image/bmp", "image/x-ms-bmp", "image/tiff":
		return true
	}
	return false
}

func convertImageToPNG(ext, detectedMIME string, data []byte) ([]byte, error) {
	var img image.Image
	var err error
	if ext == ".bmp" || detectedMIME == "image/bmp" || detectedMIME == "image/x-ms-bmp" {
		img, err = bmp.Decode(bytes.NewReader(data))
	} else {
		img, err = tiff.Decode(bytes.NewReader(data))
	}
	if err != nil {
		img, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func mediaAttachmentKind(ext, detectedMIME, extMIME string) (kind, mediaMIME string) {
	switch ext {
	case ".wav", ".mp3":
		m := extMIME
		if m == "" {
			m = detectedMIME
		}
		return "audio", m
	case ".mp4", ".webm":
		m := extMIME
		if m == "" {
			m = detectedMIME
		}
		return "video", m
	}
	switch detectedMIME {
	case "audio/wav", "audio/x-wav", "audio/vnd.wave", "audio/mpeg":
		return "audio", detectedMIME
	case "video/mp4", "video/webm":
		return "video", detectedMIME
	}
	return "", ""
}

func extractOOXMLText(ext string, data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("invalid Office document: %w", err)
	}
	if len(zr.File) > oneBotMaxOfficeEntries {
		return "", fmt.Errorf("Office document has too many archive entries")
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
	}
	var out string
	switch ext {
	case ".docx":
		f := files["word/document.xml"]
		if f == nil {
			return "", fmt.Errorf("missing word/document.xml")
		}
		raw, err := readZipEntryBounded(f, oneBotMaxOfficeBytes)
		if err != nil {
			return "", err
		}
		out, err = parseDOCXDocumentXML(raw)
		if err != nil {
			return "", err
		}
	case ".xlsx":
		var shared []string
		if ss := files["xl/sharedStrings.xml"]; ss != nil {
			raw, err := readZipEntryBounded(ss, oneBotMaxOfficeBytes)
			if err != nil {
				return "", err
			}
			shared, err = parseXLSXSharedStrings(raw)
			if err != nil {
				return "", err
			}
		}
		var sheetNames []string
		for name := range files {
			if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
				sheetNames = append(sheetNames, name)
			}
		}
		sort.Slice(sheetNames, func(i, j int) bool {
			return trailingNumber(sheetNames[i], "xl/worksheets/sheet", ".xml") < trailingNumber(sheetNames[j], "xl/worksheets/sheet", ".xml")
		})
		if len(sheetNames) == 0 {
			return "", fmt.Errorf("missing worksheet XML")
		}
		var b strings.Builder
		budget := oneBotMaxOfficeBytes
		for idx, name := range sheetNames {
			if budget <= 0 {
				break
			}
			raw, err := readZipEntryBounded(files[name], budget)
			if err != nil {
				return "", err
			}
			budget -= len(raw)
			sheetText, err := parseXLSXSheetXML(raw, shared)
			if err != nil {
				return "", err
			}
			if sheetText == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			if len(sheetNames) > 1 {
				fmt.Fprintf(&b, "Sheet %d:\n", idx+1)
			}
			b.WriteString(sheetText)
		}
		out = b.String()
	case ".pptx":
		var slideNames []string
		for name := range files {
			if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
				slideNames = append(slideNames, name)
			}
		}
		sort.Slice(slideNames, func(i, j int) bool {
			return trailingNumber(slideNames[i], "ppt/slides/slide", ".xml") < trailingNumber(slideNames[j], "ppt/slides/slide", ".xml")
		})
		if len(slideNames) == 0 {
			return "", fmt.Errorf("missing slide XML")
		}
		var b strings.Builder
		budget := oneBotMaxOfficeBytes
		for idx, name := range slideNames {
			if budget <= 0 {
				break
			}
			raw, err := readZipEntryBounded(files[name], budget)
			if err != nil {
				return "", err
			}
			budget -= len(raw)
			slideText, err := parsePPTXSlideXML(raw)
			if err != nil {
				return "", err
			}
			if slideText == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			fmt.Fprintf(&b, "Slide %d:\n%s", idx+1, slideText)
		}
		out = b.String()
	}
	if len(out) > oneBotMaxTextBytes {
		out = truncateUTF8(out, oneBotMaxTextBytes)
	}
	return out, nil
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

func readZipEntryBounded(f *zip.File, limit int) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%s exceeds extracted size limit", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("%s exceeds extracted size limit", f.Name)
	}
	return data, nil
}

func trailingNumber(name, prefix, suffix string) int {
	mid := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	n, err := strconv.Atoi(mid)
	if err != nil {
		return 1 << 30
	}
	return n
}

func parseDOCXDocumentXML(raw []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out, para strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				para.Reset()
			case "t":
				inText = true
			case "tab":
				para.WriteByte('\t')
			case "br", "cr":
				para.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if out.Len() > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(para.String())
				para.Reset()
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func parseXLSXSharedStrings(raw []byte) ([]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var list []string
	var cur strings.Builder
	inItem, inText := false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inItem = true
				cur.Reset()
			case "t":
				if inItem {
					inText = true
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "si":
				list = append(list, cur.String())
				inItem = false
			}
		case xml.CharData:
			if inItem && inText {
				cur.Write(t)
			}
		}
	}
	return list, nil
}

func parseXLSXSheetXML(raw []byte, shared []string) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out strings.Builder
	var row []string
	var cellType string
	var cellVal strings.Builder
	inCell, inVal := false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = row[:0]
			case "c":
				inCell = true
				cellType = ""
				cellVal.Reset()
				for _, attr := range t.Attr {
					if attr.Name.Local == "t" {
						cellType = attr.Value
					}
				}
			case "v", "t":
				if inCell {
					inVal = true
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inVal = false
			case "c":
				val := cellVal.String()
				if cellType == "s" {
					if idx, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && idx >= 0 && idx < len(shared) {
						val = shared[idx]
					}
				}
				row = append(row, val)
				inCell = false
			case "row":
				for len(row) > 0 && row[len(row)-1] == "" {
					row = row[:len(row)-1]
				}
				if len(row) > 0 {
					if out.Len() > 0 {
						out.WriteByte('\n')
					}
					out.WriteString(strings.Join(row, "\t"))
				}
			}
		case xml.CharData:
			if inCell && inVal {
				cellVal.Write(t)
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func parsePPTXSlideXML(raw []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out, para strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				para.Reset()
			case "t":
				inText = true
			case "br":
				para.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				text := strings.TrimSpace(para.String())
				if text != "" {
					if out.Len() > 0 {
						out.WriteByte('\n')
					}
					out.WriteString(text)
				}
				para.Reset()
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func expandDroppedPaths(paths []string, limit int) ([]string, bool) {
	if limit <= 0 {
		limit = oneBotMaxAttachments
	}
	var out []string
	truncated := false
	for _, p := range paths {
		if len(out) >= limit {
			truncated = true
			break
		}
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			out = append(out, p)
			continue
		}
		root := filepath.Clean(p)
		_ = filepath.WalkDir(root, func(cur string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			name := d.Name()
			if cur != root && (strings.HasPrefix(name, ".") || (d.IsDir() && name == "node_modules")) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if len(out) >= limit {
				truncated = true
				return filepath.SkipAll
			}
			out = append(out, cur)
			return nil
		})
		if truncated {
			break
		}
	}
	return out, truncated
}

func (ob *qaState) hasOneBotAttachmentPath(canon, spaceID, rel string) bool {
	for _, item := range ob.attachments {
		if canon != "" && item.FullPath == canon {
			return true
		}
		if spaceID != "" && rel != "" && item.SpaceID == spaceID && item.RelPath == rel {
			return true
		}
	}
	return false
}

func (ob *qaState) appendOneBotAttachments(items []oneBotAttachmentItem, failures []string, truncated bool) {
	hitLimit := false
	for _, item := range items {
		if len(ob.attachments) >= oneBotMaxAttachments {
			hitLimit = true
			break
		}
		if ob.hasOneBotAttachmentPath(item.FullPath, item.SpaceID, item.RelPath) {
			continue
		}
		ob.attachments = append(ob.attachments, item)
	}
	if hitLimit || truncated {
		ob.host.notify("%s", fmt.Sprintf(i18n.T("一次最多添加 %d 个附件"), oneBotMaxAttachments))
	}
	if len(failures) > 0 {
		ob.host.notify("%s", strings.Join(failures, "\n"))
	}
	ob.open = true
	ob.historyOpen = false
	ob.focusInput = true
}

func (ob *qaState) addOneBotFiles(paths []string) {
	if len(paths) == 0 {
		return
	}
	remaining := oneBotMaxAttachments - len(ob.attachments)
	if remaining <= 0 {
		ob.host.notify("%s", fmt.Sprintf(i18n.T("一次最多添加 %d 个附件"), oneBotMaxAttachments))
		return
	}
	expanded, truncated := expandDroppedPaths(paths, remaining)
	if len(expanded) == 0 {
		if truncated {
			ob.host.notify("%s", fmt.Sprintf(i18n.T("一次最多添加 %d 个附件"), oneBotMaxAttachments))
		}
		return
	}
	seen := make(map[string]bool, len(expanded))
	var unique []string
	for _, p := range expanded {
		canon := canonicalAttachmentPath(p)
		if seen[canon] || ob.hasOneBotAttachmentPath(canon, "", "") {
			continue
		}
		seen[canon] = true
		unique = append(unique, p)
	}
	if len(unique) == 0 {
		ob.open = true
		ob.historyOpen = false
		ob.focusInput = true
		return
	}
	snap := ob.oneBotSpaceSnapshotFor(ob.qaActiveSpace())
	process := func() ([]oneBotAttachmentItem, []string) {
		var items []oneBotAttachmentItem
		var failures []string
		for _, p := range unique {
			if len(items) >= remaining {
				truncated = true
				break
			}
			item, err := buildOneBotAttachment(p, snap)
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			items = append(items, item)
		}
		return items, failures
	}
	if ob.sync || ob.host.window() == nil {
		items, failures := process()
		ob.appendOneBotAttachments(items, failures, truncated)
		return
	}
	win := ob.host.window()
	go func() {
		items, failures := process()
		win.Update(func() {
			ob.appendOneBotAttachments(items, failures, truncated)
		})
	}()
}

func (ob *qaState) addOneBotClipboardImage(pngData []byte) {
	if len(pngData) == 0 {
		return
	}
	if len(ob.attachments) >= oneBotMaxAttachments {
		ob.host.notify("%s", fmt.Sprintf(i18n.T("一次最多添加 %d 个附件"), oneBotMaxAttachments))
		return
	}
	n := 1
	used := map[string]bool{}
	for _, item := range ob.attachments {
		used[item.Part.Name] = true
	}
	name := fmt.Sprintf("pasted-image-%d.png", n)
	for used[name] {
		n++
		name = fmt.Sprintf("pasted-image-%d.png", n)
	}
	item := oneBotAttachmentItem{
		Part: agent.ContentPart{
			Type: "image",
			Name: name,
			MIME: "image/png",
			Data: "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData),
		},
		Kind: "image",
	}
	if thumb, err := ui.DecodeBitmapScaled(pngData, oneBotThumbSize); err == nil {
		item.Thumb = thumb
	}
	ob.attachments = append(ob.attachments, item)
	ob.open = true
	ob.historyOpen = false
	ob.focusInput = true
}

func (ob *qaState) oneBotDraggedRels(d dragged) []string {
	sp := ob.qaSpaceByID(d.space)
	if sp == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(rel string) {
		if rel == "" || seen[rel] || !vault.IsDocument(rel) {
			return
		}
		seen[rel] = true
		out = append(out, rel)
	}
	var walkFolder func(nodes []*vault.Node)
	walkFolder = func(nodes []*vault.Node) {
		for _, n := range nodes {
			if n.Dir {
				walkFolder(n.Children)
				continue
			}
			add(n.Path)
			if len(out) >= oneBotMaxAttachments {
				return
			}
		}
	}
	for _, rel := range ob.host.rawGroupFor(d.space, d.rel) {
		if ob.host.isDocOf(dragged{d.space, rel}) {
			add(rel)
			continue
		}
		var tree []*vault.Node
		if active := ob.qaActiveSpace(); active != nil && active.ID == d.space {
			tree = ob.host.activeTree()
		} else {
			tree = ob.host.cachedTree(d.space)
		}
		if folder := vault.Find(tree, rel); folder != nil && folder.Dir {
			walkFolder(folder.Children)
		}
	}
	return out
}

func (ob *qaState) addOneBotSpaceRefs(spaceID string, rels []string) {
	sp := ob.qaSpaceByID(spaceID)
	if sp == nil || len(rels) == 0 {
		return
	}
	remaining := oneBotMaxAttachments - len(ob.attachments)
	if remaining <= 0 {
		ob.host.notify("%s", fmt.Sprintf(i18n.T("一次最多添加 %d 个附件"), oneBotMaxAttachments))
		return
	}
	active := ob.qaActiveSpace()
	canEdit := active != nil && active.ID == sp.ID
	snap := ob.oneBotSpaceSnapshotFor(sp)

	type job struct {
		rel      string
		canon    string
		editable bool
		resident bool
		liveText string
	}
	var jobs []job
	truncated := false
	for _, rel := range rels {
		if len(jobs) >= remaining {
			truncated = true
			break
		}
		if ob.hasOneBotAttachmentPath("", sp.ID, rel) {
			continue
		}
		var canon string
		if !sp.IsExternal() {
			if full, err := resolveFile(sp, rel); err == nil {
				canon = canonicalAttachmentPath(full)
			}
		}
		if ob.hasOneBotAttachmentPath(canon, sp.ID, rel) {
			continue
		}
		j := job{
			rel:      rel,
			canon:    canon,
			editable: canEdit && isEditableSpaceDoc(rel, snap.ReadOnly[rel]),
		}
		if live, ok := snap.Live[rel]; ok {
			j.resident = true
			j.liveText = live
		} else if sp.IsExternal() && sp.ExternalFiles != nil {
			if p, ok := sp.ExternalFiles[rel]; ok && p != "" {
				j.canon = canonicalAttachmentPath(p)
			}
		}
		jobs = append(jobs, j)
	}
	if len(jobs) == 0 {
		if truncated {
			ob.host.notify("%s", fmt.Sprintf(i18n.T("一次最多添加 %d 个附件"), oneBotMaxAttachments))
		}
		ob.open = true
		ob.historyOpen = false
		ob.focusInput = true
		return
	}
	spCopy := *sp
	process := func() ([]oneBotAttachmentItem, []string) {
		var items []oneBotAttachmentItem
		var failures []string
		for _, j := range jobs {
			var item oneBotAttachmentItem
			var err error
			switch {
			case j.resident:
				item = oneBotAttachmentItem{
					Part: agent.ContentPart{
						Type: "text",
						Name: path.Base(j.rel),
						MIME: "text/markdown",
						Text: formatAttachedFileText(path.Base(j.rel), j.rel, j.editable, j.liveText),
					},
					SpaceID:  spCopy.ID,
					RelPath:  j.rel,
					FullPath: j.canon,
					Kind:     "doc",
					Editable: j.editable,
				}
			case j.canon != "":
				item, err = buildOneBotAttachment(j.canon, snap)
				if err == nil {
					item.SpaceID = spCopy.ID
					item.RelPath = j.rel
					item.Editable = j.editable
				}
			default:
				text, readErr := readFile(&spCopy, j.rel)
				if readErr != nil {
					err = readErr
				} else {
					item, err = parseOneBotAttachmentData(path.Base(j.rel), strings.ToLower(path.Ext(j.rel)), j.canon, spCopy.ID, j.rel, j.editable, []byte(text), snap)
				}
			}
			if err != nil {
				failures = append(failures, err.Error())
				continue
			}
			items = append(items, item)
		}
		return items, failures
	}
	if ob.sync || ob.host.window() == nil {
		items, failures := process()
		ob.appendOneBotAttachments(items, failures, truncated)
		return
	}
	win := ob.host.window()
	go func() {
		items, failures := process()
		win.Update(func() {
			ob.appendOneBotAttachments(items, failures, truncated)
		})
	}()
}

func (ob *qaState) pickOneBotAttachments() {
	done := func(paths []string) {
		ob.addOneBotFiles(paths)
	}
	if pick := ob.host.filePicker(); pick != nil {
		pick(done)
		return
	}
	if ob.host.window() == nil {
		return
	}
	win := ob.host.window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: i18n.T("添加参考附件"), Multiple: true})
		if err != nil || len(paths) == 0 {
			return
		}
		win.Update(func() {
			ob.addOneBotFiles(paths)
		})
	}()
}
