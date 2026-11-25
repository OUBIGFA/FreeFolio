package editor

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/document"
)

// MergeFiles adapts the shared block merge to the editor's lossless Markdown
// codec. Unsupported files and overlapping edits stay on the conflict path.
func MergeFiles(name string, base, ours, theirs []byte) ([]byte, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".md" && ext != ".arcmd" {
		return nil, false
	}
	parse := func(raw []byte) (*document.Document, error) {
		if ext == ".arcmd" {
			return document.Decode(string(raw))
		}
		return ImportMarkdown(string(raw)), nil
	}
	var docs [3]*document.Document
	for i, raw := range [][]byte{base, ours, theirs} {
		if !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
			return nil, false
		}
		d, e := parse(raw)
		if e != nil {
			return nil, false
		}
		docs[i] = d
	}
	m, conflicts := document.MergeDocuments(docs[0], docs[1], docs[2])
	if len(conflicts) > 0 {
		return nil, false
	}
	if m.Validate() != nil {
		return nil, false
	}
	if ext == ".arcmd" {
		s, e := m.Encode()
		return []byte(s), e == nil
	}
	return []byte(ExportMarkdown(m)), true
}
