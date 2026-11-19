package editor

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseProperties(t *testing.T) {
	fm := "title:\n" +
		"date: 2026-05-24\n" +
		"# a comment\n" +
		"tag:\n  - Evergreen\n  - \"历史存档\"\n" +
		"aliases: [one, 'two']\n" +
		"description: 什么时候: 自由\n" +
		"note: |\n  first line\n  second line\n" +
		"show: true\n"
	got := parseProperties(fm)
	want := []property{
		{key: "title"},
		{key: "date", values: []string{"2026-05-24"}},
		{key: "tag", values: []string{"Evergreen", "历史存档"}, list: true},
		{key: "aliases", values: []string{"one", "two"}, list: true},
		{key: "description", values: []string{"什么时候: 自由"}},
		{key: "note", values: []string{"first line second line"}},
		{key: "show", values: []string{"true"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if parseProperties("") != nil {
		t.Fatal("no frontmatter, no properties")
	}
}

// The properties show above the first block, unfolded at first; their
// toggle folds them to one row and the blocks move up with them.
func TestPropertiesShowAndFold(t *testing.T) {
	e, tt := newTester("---\ntitle: Notes\ntag:\n  - a\n  - b\ncover: gradient:sea\n---\n\nBody text")
	tt.Frame()
	if len(e.properties()) != 2 || !tt.HasText("Notes") || !tt.HasText("tag") {
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
	if !e.propsFolded || tt.HasText("Notes") {
		t.Fatal("the toggle folds the properties")
	}
	if folded := e.rows[0].rect.Y; folded != open-2*propsRowH {
		t.Fatalf("first block at %v once folded, %v open", folded, open)
	}
	// Folding only shows less: the frontmatter is the document's as read.
	if md := ExportMarkdown(e.Doc); !strings.HasPrefix(md, "---\n") || !strings.Contains(md, "tag:\n  - a\n  - b") {
		t.Fatalf("frontmatter changed: %q", md)
	}
}
