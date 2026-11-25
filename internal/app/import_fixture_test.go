package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/noteimport"
)

func TestSiYuanImportedFixtureUsesNativeBlocks(t *testing.T) {
	source := t.TempDir()
	fixture := filepath.Join("..", "noteimport", "testdata", "siyuan")
	if e := filepath.WalkDir(fixture, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(fixture, p)
		dst := filepath.Join(source, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(dst, b, 0600)
	}); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"说明书-20240101000000-fileaaa.pdf", "示意图-20240101000000-imagaaa.png", "片段-20240101000000-videoaa.mp4", "封面-20240101000000-coveraa.png"} {
		p := filepath.Join(source, "assets", name)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte("isolated attachment fixture"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	destination := t.TempDir()
	r, e := noteimport.ImportWith(source, destination, editor.CanonicalizeImportedNote)
	if e != nil {
		t.Fatal(e)
	}
	if r.Documents != 4 || r.Attachments != 4 {
		t.Fatalf("import counts: %+v", r)
	}
	content, e := os.ReadFile(filepath.Join(destination, r.Folder, "读书笔记", "全部块类型.md"))
	if e != nil {
		t.Fatal(e)
	}
	doc := editor.ImportMarkdown(string(content))
	if e := doc.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"粗斜", "备注词", "第一项", "藏起来的子项", "做完的事", "注意", "三体", "合并", "左栏", "右栏", "引用目标", "块备注"} {
		if !strings.Contains(doc.PlainText(), want) {
			t.Errorf("missing %q", want)
		}
	}
	counts := map[string]int{}
	var walk func(*model.Block)
	walk = func(b *model.Block) {
		counts[b.Kind]++
		for _, ch := range b.Children {
			walk(ch)
		}
	}
	for _, b := range doc.Blocks {
		walk(b)
	}
	for _, kind := range []string{"table", "columns", "code", "image"} {
		if counts[kind] == 0 {
			t.Errorf("missing native %s: %s", kind, content)
		}
	}
	merged := false
	var check func(*model.Block)
	check = func(b *model.Block) {
		if b.ColSpan > 1 {
			merged = true
		}
		for _, ch := range b.Children {
			check(ch)
		}
	}
	for _, b := range doc.Blocks {
		check(b)
	}
	if !merged {
		t.Error("merged table cell lost")
	}
}
