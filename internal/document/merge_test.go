package document

import "testing"

func mergeDoc(texts ...string) *Document {
	d := New()
	d.Blocks = nil
	for _, s := range texts {
		d.Blocks = append(d.Blocks, NewBlock("paragraph", s))
	}
	return d
}

func TestMergeIndependentParagraphsAndNestedChanges(t *testing.T) {
	b := mergeDoc("first paragraph", "middle", "last paragraph")
	o, r := b.Clone(), b.Clone()
	o.Blocks[0].Runs[0].Text = "first paragraph locally edited"
	r.Blocks[2].Runs[0].Text = "last paragraph remotely edited"
	m, conflicts := MergeDocuments(b, o, r)
	if len(conflicts) != 0 || m.Blocks[0].Text() != "first paragraph locally edited" || m.Blocks[2].Text() != "last paragraph remotely edited" {
		t.Fatalf("merge: %+v, %s", conflicts, m.PlainText())
	}
	if b.Blocks[0].Text() != "first paragraph" || o.Blocks[2].Text() != "last paragraph" {
		t.Fatal("merge mutated its inputs")
	}
	b.Blocks = []*Block{{ID: NewID(), Kind: "columns", Children: []*Block{{ID: NewID(), Kind: "column", Children: []*Block{NewBlock("paragraph", "left")}}, {ID: NewID(), Kind: "column", Children: []*Block{NewBlock("paragraph", "right")}}}}}
	o, r = b.Clone(), b.Clone()
	o.Blocks[0].Children[0].Children[0].Runs[0].Text = "local left"
	r.Blocks[0].Children[1].Children[0].Runs[0].Text = "remote right"
	m, conflicts = MergeDocuments(b, o, r)
	if len(conflicts) != 0 || m.Blocks[0].Children[0].Children[0].Text() != "local left" || m.Blocks[0].Children[1].Children[0].Text() != "remote right" {
		t.Fatalf("nested merge: %+v", conflicts)
	}
}

func TestMergeConflictsAndConcurrentInsertions(t *testing.T) {
	b := mergeDoc("shared paragraph", "anchor")
	o, r := b.Clone(), b.Clone()
	o.Blocks[0].Runs[0].Text = "local paragraph"
	r.Blocks[0].Runs[0].Text = "remote paragraph"
	m, conflicts := MergeDocuments(b, o, r)
	if len(conflicts) == 0 || m.Blocks[0].Text() != "local paragraph" {
		t.Fatal("overlapping edits must keep local and report a conflict")
	}
	o, r = b.Clone(), b.Clone()
	o.Blocks = append([]*Block{NewBlock("paragraph", "local insertion")}, o.Blocks...)
	r.Blocks = append([]*Block{NewBlock("paragraph", "remote insertion")}, r.Blocks...)
	m, conflicts = MergeDocuments(b, o, r)
	if len(conflicts) != 0 || len(m.Blocks) != 4 || m.Blocks[0].Text() != "local insertion" || m.Blocks[1].Text() != "remote insertion" {
		t.Fatalf("insertions: %s %+v", m.PlainText(), conflicts)
	}
	o, r = b.Clone(), b.Clone()
	o.Blocks = o.Blocks[1:]
	r.Blocks[0].Runs[0].Text = "remote paragraph"
	_, conflicts = MergeDocuments(b, o, r)
	if len(conflicts) == 0 {
		t.Fatal("delete versus edit silently resolved")
	}
}

func TestMergeMetadataAndIndependentIDs(t *testing.T) {
	b, o, r := mergeDoc("same"), mergeDoc("same"), mergeDoc("same")
	o.Icon = "🌿"
	r.Cover = "assets/cover.png"
	m, conflicts := MergeDocuments(b, o, r)
	if len(conflicts) != 0 || m.Icon != "🌿" || m.Cover != "assets/cover.png" {
		t.Fatalf("metadata: %+v %+v", m, conflicts)
	}
	o.Frontmatter, r.Frontmatter = "a: 1", "a: 2"
	_, conflicts = MergeDocuments(b, o, r)
	if len(conflicts) == 0 {
		t.Fatal("frontmatter conflict was dropped")
	}
}

func TestMergeUnchangedSidePreservesEveryEdit(t *testing.T) {
	base := mergeDoc("a", "b", "c", "d")
	variants := []*Document{mergeDoc("x", "b", "c", "d"), mergeDoc("a", "c", "d"), mergeDoc("a", "b", "c", "d", "new"), mergeDoc("b", "a", "c", "d"), mergeDoc("a", "b", "new", "c", "d")}
	for _, changed := range variants {
		for _, reverse := range []bool{false, true} {
			ours, theirs := changed, base.Clone()
			if reverse {
				ours, theirs = theirs, ours
			}
			merged, conflicts := MergeDocuments(base, ours, theirs)
			if len(conflicts) != 0 || merged.PlainText() != changed.PlainText() {
				t.Fatalf("unchanged side altered result: %q versus %q; %+v", merged.PlainText(), changed.PlainText(), conflicts)
			}
		}
	}
}
