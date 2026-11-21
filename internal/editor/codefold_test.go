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
	if folded > 40+codeFoldLines*22+34+codeFoldLane+1 {
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
	// Open and longer than the view, the toggle waits at the view's foot,
	// so the block folds from wherever it is read.
	for i := range e.decorations {
		if dc := *e.indexedDecoration(i); dc.id == long.ID {
			d = dc
		}
	}
	b = e.codeFoldButton(d)
	if d.rect.Y+d.rect.H <= e.Scroll.Y+e.viewport || b.Y+b.H != e.Scroll.Y+e.viewport-codeFoldMargin {
		t.Fatalf("toggle %+v, view %v..%v, block %+v", b, e.Scroll.Y, e.Scroll.Y+e.viewport, d.rect)
	}
	// With the block's foot in view, it goes back to its lane below the code.
	e.Scroll.Y = d.rect.Y + d.rect.H - e.viewport/2
	if b = e.codeFoldButton(d); b.Y+b.H != d.rect.Y+d.rect.H-codeFoldMargin {
		t.Fatalf("toggle %+v back in its lane, view %v, block %+v", b, e.Scroll.Y, d.rect)
	}
	e.Scroll.Y = 0
	// Folding with the view scrolled past the block's top goes back to it.
	e.Scroll.Y = e.rows[0].rect.Y + 400
	tt.Frame()
	if e.Scroll.Y <= e.rows[0].rect.Y {
		t.Fatal("the view did not scroll into the block")
	}
	e.ToggleCodeFold(long.ID)
	tt.Frame()
	if !e.codeFolded(long) {
		t.Fatal("folds again")
	}
	if top := e.rows[0].rect.Y; e.Scroll.Y > top {
		t.Fatalf("scroll %v stays past the folded block's top %v", e.Scroll.Y, top)
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

// The toggle of a foldable block has a lane of its own below the code, so
// it covers no text, open or folded, and keeps a margin from the block's
// foot. An open block whose foot is out of view keeps its toggle at the
// view's foot instead (TestLongCodeFoldsAndItsToggleFollowsTheBlock), so
// the open case looks with the foot in view.
func TestCodeFoldToggleHasItsOwnLane(t *testing.T) {
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, strings.Repeat("x", 200))
	}
	e, tt := newTester("```text\n" + strings.Join(lines, "\n") + "\n```\n\nafter")
	tt.Frame()
	b := e.Doc.Blocks[0]
	for _, open := range []bool{false, true} {
		if open {
			e.ToggleCodeFold(b.ID)
			tt.Frame()
		}
		r := e.rows[0]
		textBottom := r.y + r.foldH
		if r.foldH == 0 {
			_, h := r.layout.Size()
			textBottom = r.y + h
		}
		var d decoration
		for i := range e.decorations {
			if dc := *e.indexedDecoration(i); dc.id == b.ID {
				d = dc
			}
		}
		if open {
			e.Scroll.Y = max(0, d.rect.Y+d.rect.H-e.viewport/2)
		}
		btn := e.codeFoldButton(d)
		if btn.Y < textBottom+codeFoldMargin-0.5 {
			t.Errorf("open=%v: toggle %+v over the text ending at %v", open, btn, textBottom)
		}
		if foot := d.rect.Y + d.rect.H; btn.Y+btn.H > foot-codeFoldMargin+0.5 {
			t.Errorf("open=%v: toggle %+v within %v of the foot %v", open, btn, codeFoldMargin, foot)
		}
	}
}

// A long document lays out offscreen blocks by estimate and shapes them
// only as they scroll into view. A code block shaped that way must stand
// folded as its toggle says, with the toggle's lane, as it does when laid
// out at once.
func TestVirtualizedCodeBlockKeepsItsFold(t *testing.T) {
	var parts []string
	for i := range 900 {
		parts = append(parts, fmt.Sprintf("paragraph %d", i))
	}
	var lines []string
	for i := 1; i <= 45; i++ {
		lines = append(lines, fmt.Sprintf(`"key_%d": "%s",`, i, strings.Repeat("a long wrapped value ", 6)))
	}
	parts = append(parts, "```json\n"+strings.Join(lines, "\n")+"\n```", "after")
	e, tt := newTester(strings.Join(parts, "\n\n"))
	if !e.shouldVirtualize() {
		t.Fatal("the document is not virtualized")
	}
	code := e.Doc.Blocks[900]
	idx := e.rowIDs[code.ID]
	if e.indexedRow(idx).layout != nil {
		t.Fatal("the code block was shaped before it came into view")
	}
	e.Scroll.Y = e.indexedRow(idx).rect.Y - 100
	tt.Frame()
	tt.Frame()
	r := *e.indexedRow(idx)
	if r.layout == nil {
		t.Fatal("the code block was not shaped in view")
	}
	d := *e.indexedDecoration(e.decorationIDs[code.ID])
	if !d.foldable || !d.folded {
		t.Fatalf("decoration %+v not folded", d)
	}
	// It stands as tall as the same block laid out at once.
	eager, _ := newTester(parts[900])
	if want := eager.rows[0].rect.H; r.foldH == 0 || r.rect.H != want {
		t.Fatalf("toggle says folded, but the row is %v tall (fold %v), want %v", r.rect.H, r.foldH, want)
	}
	if btn := e.codeFoldButton(d); btn.Y < r.y+r.foldH+codeFoldMargin-0.5 {
		t.Fatalf("toggle %+v over the folded text ending at %v", btn, r.y+r.foldH)
	}
	// Its toggle opens and folds it again, its label following.
	for _, open := range []bool{true, false} {
		e.ToggleCodeFold(code.ID)
		tt.Frame()
		r, d := *e.indexedRow(idx), *e.indexedDecoration(e.decorationIDs[code.ID])
		if d.folded == open || (r.foldH == 0) != open || open && r.rect.H <= eager.rows[0].rect.H {
			t.Fatalf("open=%v: row %v (fold %v), decoration folded=%v", open, r.rect.H, r.foldH, d.folded)
		}
	}
}

// A long page shows buttons to its top and end in its right margin, clear
// of the text column; a short one shows none. The end is the page's true
// end even where a long page laid it out by estimate, and quick trips
// back and forth over pictures that cannot show leave the page working.
func TestScrollEndsOnLongPages(t *testing.T) {
	_, short := newTester("one\n\ntwo")
	if _, ok := short.Find("回到顶部"); ok {
		t.Fatal("a short page has the buttons")
	}
	for _, n := range []int{200, 1200} {
		var parts []string
		for i := range n {
			parts = append(parts, fmt.Sprintf("paragraph %d", i))
			if i%40 == 0 {
				parts = append(parts, fmt.Sprintf("![missing %d](missing-%d.png)", i, i), fmt.Sprintf("![gone %d](gone-%d.png)", i, i))
			}
		}
		parts = append(parts, "the last line")
		e, tt := newTester(strings.Join(parts, "\n\n"))
		tt.Frame()
		top, ok := tt.Find("回到顶部")
		if !ok {
			t.Fatalf("%d: a long page has no buttons", n)
		}
		if top.X < 900-e.padding || top.Y < 650-100 {
			t.Fatalf("%d: buttons at %+v, not in the right margin past %v", n, top, 900-e.padding)
		}
		for range 6 {
			for _, label := range []string{"回到底部", "回到顶部"} {
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
			}
		}
		if err := tt.Click("回到底部"); err != nil {
			t.Fatal(err)
		}
		for range 10 {
			tt.Frame()
		}
		if e.scrollTrip.dir != 0 || e.Scroll.Y < e.outlineState.scrollMax-1 || !lastInView(e) {
			t.Fatalf("%d: at %v of %v, trip %+v", n, e.Scroll.Y, e.outlineState.scrollMax, e.scrollTrip)
		}
		if err := tt.Click("回到顶部"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if e.Scroll.Y != 0 {
			t.Fatalf("%d: back at %v, not the top", n, e.Scroll.Y)
		}
	}
}

// lastInView reports whether the page's last block shows in the view.
func lastInView(e *Editor) bool {
	last := e.Doc.Blocks[len(e.Doc.Blocks)-1].ID
	r := e.indexedRow(e.rowIDs[last]).rect
	return r.Y >= e.Scroll.Y && r.Y+r.H <= e.Scroll.Y+e.viewport+1
}
