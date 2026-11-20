package document

import (
	"errors"
	"testing"
)

func TestAIEditReopenedDeletionRestoresAtOriginalNeighbor(t *testing.T) {
	d := New()
	d.Blocks = []*Block{NewBlock("paragraph", "first"), NewBlock("paragraph", "deleted"), NewBlock("paragraph", "last")}
	e := NewEngine(d)
	target, _ := e.CaptureEdit(6, 13)
	receipt, err := e.ApplyAI([]EditOperation{{Kind: "replace_snapshot", Target: target}})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range e.Doc.Blocks {
		b.ID = NewID()
	}
	e.Doc.Blocks = append(e.Doc.Blocks, NewBlock("paragraph", "later manual addition"))
	e.InvalidateIndex()
	if err = e.RevertAI(receipt); err != nil {
		t.Fatal(err)
	}
	if e.Doc.PlainText() != "first\ndeleted\nlast\nlater manual addition" {
		t.Fatal("restored at wrong location", e.Doc.PlainText())
	}
}

func TestAIEditFollowsStableTargetAndKeepsStyles(t *testing.T) {
	d := New()
	d.Blocks = []*Block{NewBlock("paragraph", "before"), NewBlock("paragraph", ""), NewBlock("paragraph", "same text")}
	d.Blocks[1].Runs = []Run{{Text: "hello "}, {Text: "bold", Style: Style{Bold: true}}, {Text: " world"}}
	e := NewEngine(d)
	leaf := e.LeafAtIndex(1)
	target, err := e.CaptureEdit(leaf.Start, leaf.End)
	if err != nil {
		t.Fatal(err)
	}
	e.Replace(0, 6, "a longer introduction")
	receipt, err := e.ApplyAI([]EditOperation{{Kind: "replace_text", Target: target, Text: "hi bold world!"}})
	if err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[1].Text() != "hi bold world!" || !e.Doc.Blocks[1].Runs[1].Style.Bold || e.Doc.Blocks[2].Text() != "same text" {
		t.Fatalf("wrong edit: %+v", e.Doc.Blocks)
	}
	e.Undo()
	if e.Doc.Blocks[1].Text() != "hello bold world" || e.Doc.Blocks[0].Text() != "a longer introduction" {
		t.Fatal("undo changed unrelated text")
	}
	e.Redo()
	e.Replace(0, 1, "A")
	if err = e.RevertAI(receipt); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[0].Text() != "A longer introduction" || e.Doc.Blocks[1].Text() != "hello bold world" {
		t.Fatal("task rollback lost later manual edits")
	}
}

func TestAIEditRejectsChangedTargetAndIsAtomic(t *testing.T) {
	d := New()
	d.Blocks = []*Block{NewBlock("paragraph", "one"), NewBlock("paragraph", "two")}
	e := NewEngine(d)
	a, _ := e.CaptureEdit(0, 3)
	b, _ := e.CaptureEdit(4, 7)
	e.Replace(4, 7, "manual")
	_, err := e.ApplyAI([]EditOperation{{Kind: "replace_text", Target: a, Text: "first"}, {Kind: "replace_text", Target: b, Text: "second"}})
	if !errors.Is(err, ErrEditConflict) || e.Doc.PlainText() != "one\nmanual" {
		t.Fatalf("partial apply: %s %v", e.Doc.PlainText(), err)
	}
}

func TestAIEditUnicodePartialAndTableCell(t *testing.T) {
	d := New()
	table := Table(2, 2)
	d.Blocks = []*Block{NewBlock("paragraph", "前😀中后"), table}
	e := NewEngine(d)
	target, _ := e.CaptureEdit(1, 4)
	_, err := e.ApplyAI([]EditOperation{{Kind: "replace_text", Target: target, Text: "好"}})
	if err != nil || e.Doc.Blocks[0].Text() != "前好后" {
		t.Fatalf("UTF16 %s %v", e.Doc.PlainText(), err)
	}
	leaf := e.LeafAtIndex(1)
	cell, _ := e.CaptureEdit(leaf.Start, leaf.End)
	_, err = e.ApplyAI([]EditOperation{{Kind: "replace_text", Target: cell, Text: "cell\ntext"}})
	if err != nil || e.Doc.Blocks[1].Kind != "table" || len(e.Doc.Blocks[1].Children) != 2 {
		t.Fatalf("table damaged: %v", err)
	}
}

func TestAIEditInvalidFragmentCannotDestroyDocument(t *testing.T) {
	e := NewEngine(New())
	target, _ := e.CaptureEdit(0, 0)
	fragment := New()
	fragment.Blocks[0].Kind = "unsupported"
	_, err := e.ApplyAI([]EditOperation{{Kind: "replace_fragment", Target: target, Fragment: fragment}})
	if err == nil || e.Doc.Blocks[0].Kind != "paragraph" || e.CanUndo() {
		t.Fatal("invalid replacement entered history")
	}
}

func TestAIEditRollbackRefusesLaterTargetChanges(t *testing.T) {
	d := New()
	d.Blocks[0].Runs = []Run{{Text: "old"}}
	e := NewEngine(d)
	target, _ := e.CaptureEdit(0, 3)
	receipt, err := e.ApplyAI([]EditOperation{{Kind: "replace_text", Target: target, Text: "new"}})
	if err != nil {
		t.Fatal(err)
	}
	e.Replace(0, 3, "mine")
	if err = e.RevertAI(receipt); !errors.Is(err, ErrEditConflict) || e.Doc.PlainText() != "mine" {
		t.Fatal("rollback overwrote later writing")
	}
}

func TestAIEditPartialCellCannotReplaceWholeTable(t *testing.T) {
	d := New()
	d.Blocks = []*Block{Table(2, 2)}
	e := NewEngine(d)
	leaf := e.LeafAtIndex(0)
	target, _ := e.CaptureEdit(leaf.Start, leaf.End)
	_, err := e.ApplyAI([]EditOperation{{Kind: "replace_blocks", Target: target, Blocks: []*Block{NewBlock("paragraph", "lost table")}}})
	if err == nil || e.Doc.Blocks[0].Kind != "table" {
		t.Fatal("cell selection replaced entire table")
	}
}

func TestAIEditGuardedMetadataAndReopenRollback(t *testing.T) {
	d := New()
	d.Blocks[0].Runs = []Run{{Text: "original"}}
	d.Frontmatter = "custom: value\n"
	d.Icon = "old"
	e := NewEngine(d)
	target, _ := e.CaptureEdit(0, 8)
	receipt, err := e.ApplyAI([]EditOperation{{Kind: "replace_text", Target: target, Text: "new"}})
	if err != nil {
		t.Fatal(err)
	}
	// Markdown reload assigns fresh IDs. Recovery can rebind only unique,
	// unchanged after-blocks and must retain the new document's other fields.
	for _, b := range e.Doc.Blocks {
		b.ID = NewID()
	}
	e.InvalidateIndex()
	if err = e.RevertAI(receipt); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[0].Text() != "original" || e.Doc.Frontmatter != "custom: value\n" {
		t.Fatal("reopen rollback failed")
	}
}

func TestAIEditRichRewritePreservesUnchangedBlockProperties(t *testing.T) {
	d := New()
	d.Blocks = []*Block{NewBlock("heading", "old title"), NewBlock("paragraph", "body")}
	d.Blocks[0].Level = 2
	d.Blocks[0].Align = "center"
	d.Blocks[0].Folded = true
	d.Blocks[1].Runs = []Run{{Text: "body", Style: Style{Bold: true, Color: "blue"}}}
	e := NewEngine(d)
	target, _ := e.CaptureEdit(0, e.TextLength())
	fragment := New()
	fragment.Blocks = []*Block{NewBlock("heading", "new title"), NewBlock("paragraph", "body")}
	fragment.Blocks[0].Level = 2
	_, err := e.ApplyAI([]EditOperation{{Kind: "replace_fragment", Target: target, Fragment: fragment}})
	if err != nil || e.Doc.Blocks[0].Align != "center" || !e.Doc.Blocks[0].Folded || !e.Doc.Blocks[1].Runs[0].Style.Bold || e.Doc.Blocks[1].Runs[0].Style.Color != "blue" {
		t.Fatalf("properties lost %+v %v", e.Doc.Blocks, err)
	}
}

func TestAIEditMetadataRequiresSnapshotAndKeepsBody(t *testing.T) {
	d := New()
	d.Blocks[0].Runs = []Run{{Text: "body"}}
	d.Frontmatter = "tag: old\n"
	e := NewEngine(d)
	target, _ := e.CaptureEdit(0, 4)
	meta := PageMetadataOf(e.Doc)
	goal := 1200
	r, err := e.ApplyAI([]EditOperation{{Kind: "update_metadata", Target: target, ExpectedMetadata: &meta, Metadata: &MetadataEdit{Goal: &goal}}})
	if err != nil || e.Doc.Goal != 1200 || e.Doc.Frontmatter != "tag: old\n" || e.Doc.PlainText() != "body" {
		t.Fatalf("metadata: %+v %v", e.Doc, err)
	}
	if err = e.RevertAI(r); err != nil || e.Doc.Goal != 0 {
		t.Fatalf("metadata rollback %v", err)
	}
	e.Doc.Goal = 99
	if _, err = e.ApplyAI([]EditOperation{{Kind: "update_metadata", Target: target, ExpectedMetadata: &meta, Metadata: &MetadataEdit{Goal: &goal}}}); !errors.Is(err, ErrEditConflict) {
		t.Fatal("stale metadata accepted")
	}
}
