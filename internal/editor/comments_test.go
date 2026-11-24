package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/document"
)

// Comments belong to the saved source even when adjacent visible text changes.
func TestInlineCommentsSurviveEditingUndoAndExport(t *testing.T) {
	for _, src := range []string{
		"alpha <!-- hidden --> omega\n",
		"中文😀 <!-- hidden --> 后文\n",
		"<!-- first -->Visible\n",
		"| A | B |\n| --- | --- |\n| x <!-- hidden --> y | z |\n",
	} {
		t.Run(src, func(t *testing.T) {
			e := New(src)
			if strings.Contains(e.Text(), "<!--") {
				t.Fatalf("comment is visible in editor text: %q", e.Text())
			}
			e.SetSelection(0, 0)
			e.ReplaceText(nil, "X")
			saved := ExportMarkdown(e.Committed())
			if !strings.Contains(saved, "<!--") || strings.Contains(saved, "&lt;!--") {
				t.Fatalf("comment markup changed: %q", saved)
			}
			if err := e.Committed().Validate(); err != nil {
				t.Fatal(err)
			}
			e.Undo()
			if got := ExportMarkdown(e.Committed()); got != src {
				t.Fatalf("undo did not restore original source: %q", got)
			}
			if got := ExportHTML(e.Committed()); strings.Contains(got, "hidden") || strings.Contains(got, "<!-- first -->") {
				t.Fatalf("comment leaked into export: %q", got)
			}
		})
	}
}

func TestCommentEditsKeepOffsetsAcrossSplitAndJoin(t *testing.T) {
	e := New("甲乙 <!-- hidden --> 丙丁\n")
	e.SetSelection(document.Len("甲"), document.Len("甲"))
	e.Engine.Enter(false)
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := ExportMarkdown(e.Doc); !strings.Contains(got, "<!-- hidden -->") {
		t.Fatalf("split dropped comment: %q", got)
	}
	e.Engine.Join(true)
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := ExportMarkdown(e.Doc); !strings.Contains(got, "<!-- hidden -->") {
		t.Fatalf("join dropped comment: %q", got)
	}
}
