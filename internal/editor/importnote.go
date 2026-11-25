package editor

import (
	"github.com/OUBIGFA/FreeFolio/internal/document"
	"strings"
)

// CanonicalizeImportedNote converts generated migration markup through the
// native HTML/table/column codecs. It is never used for original Markdown files,
// whose unedited bytes must stay intact. Only our known generated wrappers are
// converted; arbitrary HTML remains a visible raw block.
func CanonicalizeImportedNote(source string) (string, error) {
	doc := ImportMarkdown(source)
	var out []*document.Block
	for _, b := range doc.Blocks {
		if b.Kind == "raw" {
			raw := strings.TrimSpace(b.Text())
			if strings.HasPrefix(raw, "<table>") || strings.HasPrefix(raw, `<div class="freefolio-columns"`) || strings.HasPrefix(raw, `<p style="`) {
				converted := ImportHTML(raw)
				if e := converted.Validate(); e != nil {
					return "", e
				}
				out = append(out, converted.Blocks...)
				continue
			}
		}
		out = append(out, b)
	}
	doc.Blocks = out
	doc.ImportSource = ""
	if e := doc.Validate(); e != nil {
		return "", e
	}
	return ExportMarkdown(doc), nil
}
