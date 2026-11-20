package editor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestParseProperties(t *testing.T) {
	fm := "title:\n" +
		"date: 2026-05-24\n" +
		"# a comment\n" +
		"tag:\n  - Evergreen\n  - \"历史存档\"\n" +
		"aliases: [one, 'two']\n" +
		"description: 什么时候: 自由\n" +
		"note: |\n  first line\n  second line\n" +
		"show: true\n" +
		"meta:\n  a: 1\n" +
		"ref: &x anchor\n"
	got := parseProperties(fm)
	want := []property{
		{key: "title", start: 0, end: 1},
		{key: "date", values: []string{"2026-05-24"}, start: 1, end: 2},
		{key: "tag", values: []string{"Evergreen", "历史存档"}, list: true, start: 3, end: 6},
		{key: "aliases", values: []string{"one", "two"}, list: true, start: 6, end: 7},
		{key: "description", values: []string{"什么时候: 自由"}, start: 7, end: 8},
		{key: "note", values: []string{"first line second line"}, start: 8, end: 11, fixed: true},
		{key: "show", values: []string{"true"}, start: 11, end: 12},
		{key: "meta", values: []string{"a: 1"}, start: 12, end: 14, fixed: true},
		{key: "ref", values: []string{"&x anchor"}, start: 14, end: 15, fixed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if parseProperties("") != nil {
		t.Fatal("no frontmatter, no properties")
	}
}

// Editing a property rewrites its lines only: comments and the other keys
// keep their text, and the history takes each edit back in one step.
func TestPropertyEditsKeepOtherLines(t *testing.T) {
	md := "---\n# kept comment\ntitle: Notes\ntags: [a, b]\nnote: |\n  keep me\nflag: false\n---\n\nBody"
	e, _ := newTester(md)
	e.properties()
	at := func(key string) int {
		for i, p := range e.properties() {
			if p.key == key {
				return i
			}
		}
		t.Fatalf("no property %q in %+v", key, e.properties())
		return -1
	}
	e.setProperty(at("title"), []string{"Notes: draft"}, false)
	e.setProperty(at("tags"), []string{"a", "b", "c"}, true)
	e.renameProperty(at("flag"), "done")
	e.setProperty(at("done"), []string{"true"}, false)
	want := "# kept comment\ntitle: \"Notes: draft\"\ntags:\n  - a\n  - b\n  - c\nnote: |\n  keep me\ndone: true"
	if e.Doc.Frontmatter != want {
		t.Fatalf("frontmatter %q\nwant %q", e.Doc.Frontmatter, want)
	}
	if !e.addProperty("status") || e.addProperty("status") || e.addProperty("icon") || e.addProperty("a: b") {
		t.Fatal("a new name, then the same, a reserved and a broken one")
	}
	e.deleteProperty(at("title"))
	if strings.Contains(e.Doc.Frontmatter, "title") || !strings.HasSuffix(e.Doc.Frontmatter, "done: true\nstatus:") {
		t.Fatalf("delete and add: %q", e.Doc.Frontmatter)
	}
	md2 := ExportMarkdown(e.Doc)
	if !strings.HasPrefix(md2, "---\n# kept comment\n") || !strings.Contains(md2, "note: |\n  keep me\n") {
		t.Fatalf("exported %q", md2)
	}
	for range 6 {
		e.Undo()
	}
	if e.Doc.Frontmatter != "# kept comment\ntitle: Notes\ntags: [a, b]\nnote: |\n  keep me\nflag: false" {
		t.Fatalf("undone to %q", e.Doc.Frontmatter)
	}
}

// The properties show above the first block, unfolded at first; their
// toggle folds them to one row and the blocks move up with them.
func TestPropertiesShowAndFold(t *testing.T) {
	e, tt := newTester("---\ntitle: Notes\ntag:\n  - a\n  - b\ncover: gradient:sea\n---\n\nBody text")
	tt.Frame()
	if len(e.properties()) != 2 || !tt.HasText("tag") {
		t.Fatalf("properties %+v not shown", e.properties())
	}
	open := e.rows[0].rect.Y
	if r := e.header.propsRect; r.H == 0 || open < r.Y+r.H {
		t.Fatalf("first block at %v overlaps the properties %+v", open, r)
	}
	if err := tt.Click("笔记属性"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !e.propsFolded || tt.HasText("tag") {
		t.Fatal("the toggle folds the properties")
	}
	if folded := e.rows[0].rect.Y; folded >= open {
		t.Fatalf("first block at %v once folded, %v open", folded, open)
	}
	// Folding only shows less: the frontmatter is the document's as read.
	if md := ExportMarkdown(e.Doc); !strings.HasPrefix(md, "---\n") || !strings.Contains(md, "tag:\n  - a\n  - b") {
		t.Fatalf("frontmatter changed: %q", md)
	}
}

// The list's fields edit the properties: text commits on Enter, a check box
// on a click, a new property takes the name typed; a property only shown
// has no field, and a locked page shows them all without fields.
func TestPropertiesEditInPlace(t *testing.T) {
	e, tt := newTester("---\ntitle: Notes\ndone: false\nnote: |\n  keep\n---\n\nBody")
	tt.Frame()
	tt.Frame()
	field, ok := tt.Find("title")
	if !ok {
		t.Fatal("no field for the title")
	}
	tt.ClickAt(field.X+field.W-4, field.Y+field.H/2)
	tt.Frame()
	tt.Type(" v2")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !strings.Contains(e.Doc.Frontmatter, "title: Notes v2") {
		t.Fatalf("text not committed: %q", e.Doc.Frontmatter)
	}
	if err := tt.Click("done"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(e.Doc.Frontmatter, "done: true") {
		t.Fatalf("check box not committed: %q", e.Doc.Frontmatter)
	}
	if _, ok := e.propsEdit.focused["value:note"]; ok || !tt.HasText("note") {
		t.Fatal("a block scalar has a field, or does not show")
	}
	if err := tt.Click("添加属性"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	tt.Type("status")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !strings.HasSuffix(e.Doc.Frontmatter, "\nstatus:") || e.propsEdit.adding {
		t.Fatalf("property not added: %q", e.Doc.Frontmatter)
	}
	tt.Frame()
	if err := tt.Click("删除属性"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if strings.Contains(e.Doc.Frontmatter, "title") {
		t.Fatalf("property not deleted: %q", e.Doc.Frontmatter)
	}
	e.SetReadOnly(true)
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("删除属性"); ok || !tt.HasText("done") {
		t.Fatal("a locked page edits its properties")
	}
}

// A list property edits as chips: Enter and a comma add what was typed,
// a chip's × takes the tag out, and the input empties after each.
func TestTagListAddsAndRemoves(t *testing.T) {
	e, tt := newTester("---\ntags:\n  - Design\n---\n\nBody")
	tt.Frame()
	tt.Frame()
	field, ok := tt.Find("tags")
	if !ok {
		t.Fatal("no input for the tags")
	}
	tt.ClickAt(field.X+field.W/2, field.Y+field.H/2)
	tt.Frame()
	tt.Type("AI")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Type("prompt,")
	tt.Frame()
	tt.Frame()
	if got := parseProperties(e.Doc.Frontmatter); len(got) != 1 || strings.Join(got[0].values, "|") != "Design|AI|prompt" {
		t.Fatalf("tags not added: %q", e.Doc.Frontmatter)
	}
	if e.propsEdit.drafts["tags:tags"] != "" {
		t.Fatalf("input kept %q", e.propsEdit.drafts["tags:tags"])
	}
	if err := tt.Click("移除标签 AI"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := parseProperties(e.Doc.Frontmatter); strings.Join(got[0].values, "|") != "Design|prompt" {
		t.Fatalf("tag not removed: %q", e.Doc.Frontmatter)
	}
}
