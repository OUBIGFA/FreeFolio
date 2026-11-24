package document

import "testing"

func TestTypingAndEnterAfterLinkDoNotExtendLink(t *testing.T) {
	url := "https://github.com/OUBIGFA/FreeFolio/issues/20"
	d := New()
	d.Blocks = []*Block{{
		ID:   NewID(),
		Kind: "bullet",
		Runs: []Run{{Text: url, Style: Style{Link: url}}},
	}}
	e := NewEngine(d)

	// Collapsed caret at trailing edge of link types plain unlinked text.
	n := Len(url)
	e.SetSelection(n, n)
	if e.CurrentStyle().Link != "" {
		t.Fatalf("expected non-inclusive link at trailing boundary, got %q", e.CurrentStyle().Link)
	}
	e.Replace(n, n, " 已完成")
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 2 || runs[0].Style.Link != url || runs[1].Text != " 已完成" || runs[1].Style.Link != "" {
		t.Fatalf("typing after link extended link: %+v", runs)
	}

	// Undo back to pure link bullet, then press Enter at trailing edge: next bullet must be unlinked.
	e.Undo()
	e.SetSelection(n, n)
	e.Enter(false)
	e.Replace(e.Caret, e.Caret, "下一条")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[1].Runs[0].Style.Link != "" {
		t.Fatalf("Enter after link leaked link onto next block: %+v", e.Doc.Blocks)
	}

	// Typing strictly inside the link preserves Style.Link.
	e.SetSelection(8, 8)
	if e.CurrentStyle().Link != url {
		t.Fatalf("expected interior caret to keep link, got %q", e.CurrentStyle().Link)
	}
}
