package noteimport

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestMarkdownImportPreservesBytesPathsAndOriginals(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	const text = "---\r\ncustom: value\r\n---\r\n\r\n![图片](assets/a.png)\r\n"
	put(t, src, "Nested/note.md", text)
	put(t, src, "Nested/assets/a.png", "image")
	put(t, src, ".obsidian/config.json", "private")
	r, e := Import(src, dst)
	if e != nil {
		t.Fatal(e)
	}
	if r.Documents != 1 || r.Attachments != 1 {
		t.Fatalf("report: %+v", r)
	}
	b, e := os.ReadFile(filepath.Join(dst, r.Folder, "Nested/note.md"))
	if e != nil || string(b) != text {
		t.Fatalf("bytes: %q %v", b, e)
	}
	b, _ = os.ReadFile(filepath.Join(src, "Nested/note.md"))
	if string(b) != text {
		t.Fatal("source changed")
	}
	if _, e = os.Stat(filepath.Join(dst, r.Folder, ".obsidian/config.json")); !os.IsNotExist(e) {
		t.Fatal("application configuration copied")
	}
}

func TestSingleMarkdownCopiesReferencedAttachments(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	put(t, src, "note.md", "![](pictures/photo.png)\n\n![[.assets/drawing.svg]]\n")
	put(t, src, "pictures/photo.png", "image")
	put(t, src, ".assets/drawing.svg", "<svg/>")
	put(t, src, "unrelated.txt", "not selected")
	r, e := Import(filepath.Join(src, "note.md"), dst)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"pictures/photo.png", ".assets/drawing.svg"} {
		if _, e = os.Stat(filepath.Join(dst, r.Folder, filepath.FromSlash(name))); e != nil {
			t.Fatalf("missing linked file %s: %v", name, e)
		}
	}
	if _, e = os.Stat(filepath.Join(dst, r.Folder, "unrelated.txt")); !os.IsNotExist(e) {
		t.Fatal("unrelated file copied")
	}
}

func TestSiYuanImportRemapsLinksAssetsAndReportsUnknown(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	put(t, src, "data/book/.siyuan/conf.json", `{"name":"Notebook"}`)
	put(t, src, "data/book/a.sy", `{"ID":"a","Type":"NodeDocument","Properties":{"title":"First"},"Children":[{"ID":"p","Type":"NodeParagraph","Children":[{"Type":"NodeTextMark","TextMarkType":"block-ref","TextMarkBlockRefID":"b","TextMarkTextContent":"Next"},{"Type":"NodeImage","Children":[{"Type":"NodeLinkDest","Data":"assets/pic.png"}]}]},{"Type":"NodeFuture","Data":"keep this text"}]}`)
	put(t, src, "data/book/b.sy", `{"ID":"b","Type":"NodeDocument","Properties":{"title":"Second"},"Children":[{"Type":"NodeParagraph","Children":[{"Type":"NodeText","Data":"hello"}]}]}`)
	put(t, src, "data/assets/pic.png", "picture")
	r, e := Import(src, dst)
	if e != nil {
		t.Fatal(e)
	}
	if r.Documents != 2 || len(r.Issues) == 0 {
		t.Fatalf("report: %+v", r)
	}
	b, e := os.ReadFile(filepath.Join(dst, r.Folder, "Notebook/First.md"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "Second.md") || !strings.Contains(string(b), "pic.png") || !strings.Contains(string(b), "keep this text") {
		t.Fatalf("lost content: %s", b)
	}
}

func TestZipTraversalRefusedBeforeImport(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, _ := z.Create("../escape.md")
	f.Write([]byte("bad"))
	z.Close()
	src := filepath.Join(t.TempDir(), "export.sy.zip")
	if e := os.WriteFile(src, buf.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	dst := t.TempDir()
	if _, e := Import(src, dst); e == nil {
		t.Fatal("zip traversal accepted")
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Fatal("unsafe archive wrote output")
	}
}
