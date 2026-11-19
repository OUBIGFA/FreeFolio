package editor

import (
	"fmt"
	"strings"
	"testing"
)

func TestLongCodeFoldsAndItsToggleFollowsTheBlock(t *testing.T) {
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("line(%d)", i))
	}
	e, tt := newTester("```go\n" + strings.Join(lines, "\n") + "\n```\n\nafter\n\n```go\nshort()\n```")
	tt.Frame()
	long, short := e.Doc.Blocks[0], e.Doc.Blocks[2]
	if !e.codeFolded(long) || e.codeFoldable(short) {
		t.Fatal("only the long block folds")
	}
	folded := e.rows[0].rect.H
	if folded > 40+codeFoldLines*22+34+1 {
		t.Fatalf("folded height %v", folded)
	}
	var d decoration
	for i := range e.decorations {
		if dc := *e.indexedDecoration(i); dc.id == long.ID {
			d = dc
		}
	}
	if !d.foldable || !d.folded || d.lines != 40 {
		t.Fatalf("decoration %+v", d)
	}
	bounds, _ := tt.Find("文档")
	b := e.codeFoldButton(d)
	x, y := bounds.X+b.X+b.W/2, bounds.Y+b.Y+b.H/2-e.Scroll.Y
	tt.Press(x, y)
	tt.Release(x, y)
	tt.Frame()
	if e.codeFolded(long) || e.rows[0].rect.H <= folded+500 {
		t.Fatal("the toggle opens the block", e.rows[0].rect.H)
	}
	// Open and longer than the view, the toggle sits at the view's foot,
	// within the block.
	for i := range e.decorations {
		if dc := *e.indexedDecoration(i); dc.id == long.ID {
			d = dc
		}
	}
	b = e.codeFoldButton(d)
	if b.Y+b.H > e.Scroll.Y+e.viewport || b.Y+b.H < e.Scroll.Y+e.viewport-40 || b.Y+b.H > d.rect.Y+d.rect.H {
		t.Fatalf("toggle %+v, view %v..%v, block %+v", b, e.Scroll.Y, e.Scroll.Y+e.viewport, d.rect)
	}
	e.ToggleCodeFold(long.ID)
	tt.Frame()
	if !e.codeFolded(long) {
		t.Fatal("folds again")
	}
	// The caret going into the part out of sight opens the block.
	leaf := e.Engine.LeafAt(1)
	at := leaf.Start + strings.Index(long.Text(), "line(30)")
	e.SetSelection(at, at)
	tt.Frame()
	if e.codeFolded(long) {
		t.Fatal("the caret opens the block")
	}
	// Printing shows the whole block.
	e.codeOpen = nil
	e.printing = true
	if e.codeFolded(long) {
		t.Fatal("printed whole")
	}
	e.printing = false
}

// A few long lines that wrap stand as tall as many short ones and fold
// too, as the prompts of a writing guide do.
func TestWrappedCodeFoldsByItsHeight(t *testing.T) {
	var lines []string
	for i := 1; i <= 8; i++ {
		lines = append(lines, fmt.Sprintf(`{"prompt%d": "%s"}`, i, strings.Repeat("a long wrapped prompt ", 30)))
	}
	e, tt := newTester("```json\n" + strings.Join(lines, "\n") + "\n```\n\nafter")
	tt.Frame()
	b := e.Doc.Blocks[0]
	if codeLines(b, 1<<30) > codeFoldLines || !e.codeFolded(b) {
		t.Fatalf("wrapped block of %d lines not folded", codeLines(b, 1<<30))
	}
	if r := e.rows[0]; r.foldH == 0 || r.layout == nil {
		t.Fatalf("row %+v keeps its whole height", r.rect)
	} else if _, full := r.layout.Size(); full <= r.foldH {
		t.Fatalf("text %v within the fold %v", full, r.foldH)
	}
	// A folded block's toggle stays at its foot even when the block only
	// enters at the foot of the view.
	var d decoration
	for i := range e.decorations {
		if dc := *e.indexedDecoration(i); dc.id == b.ID {
			d = dc
		}
	}
	e.viewport = d.rect.Y - e.Scroll.Y + 60
	if btn := e.codeFoldButton(d); btn.Y+btn.H < d.rect.Y+d.rect.H-30 {
		t.Fatalf("toggle %+v over the folded block %+v", btn, d.rect)
	}
	// One wrapped line under the fold's height stays whole.
	e, tt = newTester("```json\n" + lines[0] + "\n```")
	tt.Frame()
	if e.codeFoldable(e.Doc.Blocks[0]) {
		t.Fatal("a short block folds")
	}
}
