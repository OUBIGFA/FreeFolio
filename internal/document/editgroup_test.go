package document

import "testing"

func TestEditGroupIsOneUndoWithoutMergingUnrelatedChanges(t *testing.T) {
	e := NewEngine(New())
	id := e.Doc.Blocks[0].ID
	set := func(s string) { e.Doc.Blocks[0].Runs = []Run{{Text: s}} }
	e.EditGrouped("board-session", func() { set("first") })
	e.EditGrouped("board-session", func() { set("second") })
	e.Undo()
	if e.Doc.Blocks[0].Text() != "" {
		t.Fatal("group did not undo to original")
	}
	e.Redo()
	if e.Doc.Blocks[0].Text() != "second" {
		t.Fatal("group redo did not restore final")
	}
	e.Edit(func() { set("other") })
	e.EditGrouped("board-session", func() { set("third") })
	e.Undo()
	if e.Doc.Blocks[0].Text() != "other" || e.Doc.Blocks[0].ID != id {
		t.Fatal("group crossed unrelated edit")
	}
}
