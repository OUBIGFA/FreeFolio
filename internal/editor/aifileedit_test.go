package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/document"
)

func TestMarkdownFileEditsPreserveUntouchedBlocksAndUndo(t *testing.T) {
	source := "---\ncustom: value\n---\n\n# Heading\n\nhello **world**\n\n| A | B |\n| - | - |\n| x | y |\n\n![image](photo.png)\n\nlast\n"
	d := ImportMarkdown(source)
	d.Blocks[0].Folded = true
	e := document.NewEngine(d)
	before := e.Doc.Clone()
	after := strings.Replace(source, "hello **world**", "hi **world**", 1)
	after = strings.Replace(after, "\nlast\n", "\nlast\n\nAdded paragraph\n", 1)
	ops, err := MarkdownFileEdits(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected two review groups, got %d", len(ops))
	}
	if _, err = e.ApplyAI(ops); err != nil {
		t.Fatal(err)
	}
	if !e.Doc.Blocks[0].Folded || e.Doc.Blocks[0].ID != before.Blocks[0].ID || e.Doc.Frontmatter != before.Frontmatter {
		t.Fatal("lost untouched metadata")
	}
	for _, b := range before.Blocks {
		if b.Kind == "table" || b.Kind == "image" {
			found := false
			for _, after := range e.Doc.Blocks {
				if document.SameBlock(b, after) {
					found = true
				}
			}
			if !found {
				t.Fatal("lost untouched structure", b.Kind)
			}
		}
	}
	if !strings.Contains(ExportMarkdown(e.Doc), "hi **world**") || !strings.Contains(e.Doc.PlainText(), "Added paragraph") {
		t.Fatal("change missing")
	}
	e.Undo()
	if !document.SameDocument(before, e.Doc) {
		t.Fatal("undo did not restore native document")
	}
}

func TestMarkdownFileEditsRemoveFormattingAndMergeInsertionAnchors(t *testing.T) {
	for _, tc := range []struct{ before, after string }{
		{"**old**\n", "old\n"},
		{"middle\n", "first\n\nmiddle\n\nlast\n"},
		{"one\n\ntwo\n", "one\n\nnew\n"},
		{"# Heading\n\nbody\n", "# Heading\n"},
	} {
		t.Run(tc.before, func(t *testing.T) {
			d := ImportMarkdown(tc.before)
			e := document.NewEngine(d)
			ops, err := MarkdownFileEdits(d, tc.after)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.ApplyAI(ops); err != nil {
				t.Fatal(err)
			}
			if !sameDocumentSemantics(e.Doc, ImportMarkdown(tc.after)) {
				t.Fatalf("got %q, want %q", ExportMarkdown(e.Doc), tc.after)
			}
		})
	}
}

func TestMarkdownFileEditSourceStyleHasUndoAndRecovery(t *testing.T) {
	d := ImportMarkdown("**bold**\n")
	e := document.NewEngine(d)
	ops, err := MarkdownFileEdits(d, "__bold__\n")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.ApplyAI(ops)
	if err != nil {
		t.Fatal(err)
	}
	if ExportMarkdown(e.Doc) != "__bold__\n" {
		t.Fatal("source edit discarded")
	}
	if err = e.RevertAI(r); err != nil {
		t.Fatal(err)
	}
	if ExportMarkdown(e.Doc) != "**bold**\n" {
		t.Fatal("source recovery failed")
	}
}
