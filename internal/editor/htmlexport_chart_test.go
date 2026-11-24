package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/document"
)

// FreeMD's own diagrams beyond flowcharts and sequences, such as a pie,
// export as pictures in the editor's chart colors.
func TestExportHTMLPageDrawsPieCharts(t *testing.T) {
	doc := document.New()
	code := document.NewBlock("code", "pie title Share\n  \"A\" : 40\n  \"B\" : 60")
	code.Language = "mermaid"
	doc.Blocks = []*document.Block{code}
	out := ExportHTMLPage(doc, testHTMLTheme())
	if !strings.Contains(out, `<figure class="freemd-mermaid">`) || !strings.Contains(out, `<path d="M`) || strings.Contains(out, "pie title") {
		t.Fatalf("pie not drawn:\n%s", out)
	}
}
