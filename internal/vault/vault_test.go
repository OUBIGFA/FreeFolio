package vault

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolve(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", ".", "../x.md", "a/../../x.md", "/etc/passwd", "a/.."} {
		if _, err := Resolve(root, bad); err != ErrOutside {
			t.Errorf("Resolve(%q) = %v, want ErrOutside", bad, err)
		}
	}
	got, err := Resolve(root, "a/./b.md")
	if err != nil || got != filepath.Join(root, "a", "b.md") {
		t.Errorf("Resolve(a/./b.md) = %q, %v", got, err)
	}
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(filepath.Join(blocker, "space"), "Welcome.md"); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read through file ancestor = %v, want non-ErrNotExist error", err)
	}
}

// HasDocument sees what List sees and nothing it leaves out.
func TestHasDocument(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{".git/c.md", ".hidden.md", "img.png", "node_modules/x.md", "empty/sub/.keep"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if has, err := HasDocument(root); err != nil || has {
		t.Fatalf("only hidden and non-documents: HasDocument = %v, %v", has, err)
	}
	deep := filepath.Join(root, "empty", "sub", "Note.md")
	if err := os.WriteFile(deep, []byte("# Note"), 0o644); err != nil {
		t.Fatal(err)
	}
	if has, err := HasDocument(root); err != nil || !has {
		t.Fatalf("a nested document: HasDocument = %v, %v", has, err)
	}
	if _, err := HasDocument(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing folder must report its error")
	}
}

func TestListSkipsHiddenAndNonMarkdown(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"b.md", "A.md", "notes/z.MD", "notes/deep/y.md", "pack.mdz", ".git/c.md", ".hidden.md", "img.png", "node_modules/x.md"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	want := []string{"A.md", "b.md", "pack.mdz", "notes/z.MD", "notes/deep/y.md"}
	if !slices.Equal(paths, want) {
		t.Errorf("List = %q, want %q", paths, want)
	}
	if !IsMDZ("pack.mdz") || !IsMDZ("dir/PACK.MDZ") || IsMDZ(".mdz") || IsMDZ("note.md") {
		t.Errorf("IsMDZ classification unexpected")
	}
	if !IsDocument("pack.mdz") {
		t.Errorf("IsDocument(pack.mdz) = false, want true")
	}
}

func TestCreateWriteRead(t *testing.T) {
	root := t.TempDir()
	a, err := Create(root, "", "Idea: one/two")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Create(root, "", "Idea: one/two")
	if err != nil {
		t.Fatal(err)
	}
	if a != "Idea- one-two.md" || b != "Idea- one-two 2.md" {
		t.Errorf("Create = %q, %q", a, b)
	}
	if err := Write(root, "sub/n.md", "hello"); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(root, "sub/n.md"); err != nil || got != "hello" {
		t.Errorf("Read = %q, %v", got, err)
	}
	if Title("sub/n.md") != "n" {
		t.Errorf("Title = %q", Title("sub/n.md"))
	}
}

func TestTreeAndMoves(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"b.md", "Zed/a.md", "alpha/x.md", "alpha/deep/y.md", ".git/c.md", "img.png"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(root, "empty"), 0o755)
	nodes, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	var top []string
	for _, n := range nodes {
		top = append(top, n.Name)
	}
	if !slices.Equal(top, []string{"alpha", "empty", "Zed", "b.md"}) {
		t.Errorf("top = %q", top)
	}
	if n := Find(nodes, "alpha/deep/y.md"); n == nil || n.Title() != "y" {
		t.Errorf("Find = %+v", n)
	}

	dir, err := Mkdir(root, "alpha", "")
	if err != nil || dir != "alpha/New Folder" {
		t.Fatalf("Mkdir = %q, %v", dir, err)
	}
	moved, err := Move(root, "b.md", "alpha")
	if err != nil || moved != "alpha/b.md" {
		t.Fatalf("Move = %q, %v", moved, err)
	}
	if _, err := Move(root, "alpha", "alpha/deep"); err == nil {
		t.Error("moved a folder into itself")
	}
	renamed, err := Rename(root, "alpha/b.md", "Better")
	if err != nil || renamed != "alpha/Better.md" {
		t.Fatalf("Rename = %q, %v", renamed, err)
	}
	if _, err := Rename(root, "alpha/Better.md", "x"); err != ErrExists {
		t.Errorf("Rename onto x.md = %v, want ErrExists", err)
	}
	if renamed, err := Rename(root, "Zed", "Zeta"); err != nil || renamed != "Zeta" {
		t.Errorf("Rename folder = %q, %v", renamed, err)
	}
}

func TestNativeDocumentsAndExclusiveCreation(t *testing.T) {
	root := t.TempDir()
	first, err := CreateFile(root, "", "Note", ".arcmd", `{"version":1}`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateFile(root, "", "Note", ".arcmd", `{"version":2}`)
	if err != nil || first == second {
		t.Fatal("collision", err)
	}
	docs, err := List(root)
	if err != nil || len(docs) != 2 {
		t.Fatal("native listing", err)
	}
	renamed, err := Rename(root, first, "Changed")
	if err != nil || renamed != "Changed.arcmd" {
		t.Fatal("native rename", renamed, err)
	}
	if IsMarkdown(renamed) || !IsDocument(renamed) {
		t.Fatal("native format classification")
	}
}

func TestDatabaseDocuments(t *testing.T) {
	root := t.TempDir()
	rel, err := CreateFile(root, "Projects", "Roadmap", ExtDatabase, "# Roadmap\n")
	if err != nil || rel != "Projects/Roadmap.db.md" {
		t.Fatalf("CreateFile = %q, %v", rel, err)
	}
	if FileExt(rel) != ".db.md" {
		t.Fatalf("FileExt(%q) = %q, want .db.md", rel, FileExt(rel))
	}
	if !IsDatabase(rel) || !IsDocument(rel) || IsMarkdown(rel) || Title(rel) != "Roadmap" {
		t.Fatalf("classification/Title failed for %q", rel)
	}
	if IsDatabase(".db.md") || IsDocument(".db.md") || IsMarkdown(".db.md") {
		t.Fatal("bare .db.md should not be classified as a document")
	}
	docs, err := List(root)
	if err != nil || len(docs) != 1 || docs[0].Path != rel {
		t.Fatalf("List = %+v, %v", docs, err)
	}
	renamed, err := Rename(root, rel, "Backlog")
	if err != nil || renamed != "Projects/Backlog.db.md" {
		t.Fatalf("Rename = %q, %v", renamed, err)
	}
	if !IsDatabase(renamed) {
		t.Fatalf("renamed file lost database classification: %q", renamed)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"CON", "_CON"},
		{"con", "_con"},
		{"PRN", "_PRN"},
		{"aux.txt", "_aux.txt"},
		{"NUL.tar.gz", "_NUL.tar.gz"},
		{"lpt1.md", "_lpt1.md"},
		{"COM0", "_COM0"},
		{"com9.arcmd", "_com9.arcmd"},
		{"COM\u00b9", "_COM\u00b9"},
		{"com\u00b2.md", "_com\u00b2.md"},
		{"lpt\u00b3.txt", "_lpt\u00b3.txt"},
		{"CON.", "_CON"},
		{"CON .md", "_CON .md"},
		{"Note .", "Note"},
		{"Note . .   ", "Note"},
		{"  ...  ", ""},
		{" . . ", ""},
		{"hello\x00\n\tworld", "helloworld"},
		{"Idea: one/two", "Idea- one-two"},
		{"Connect.md", "Connect.md"},
	}
	for _, tc := range cases {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestListStatAndTreeModified(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := Write(outside, "Secret.md", "outside"); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, "Alpha.md", "hello world"); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, "Folder/Nested.arcmd", `{"version":1}`); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	hasLinks := true
	if err := os.Symlink(filepath.Join(root, "Alpha.md"), filepath.Join(root, "Alias.md")); err != nil {
		hasLinks = false
	} else {
		_ = os.Symlink(outside, filepath.Join(root, "EscapeDir"))
		_ = os.Symlink(filepath.Join(outside, "Secret.md"), filepath.Join(root, "Escape.md"))
	}

	stats, err := ListStat(root)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]DocStat{}
	for _, s := range stats {
		byPath[s.Path] = s
		full := filepath.Join(root, filepath.FromSlash(s.Path))
		fi, err := os.Stat(full)
		if err != nil {
			t.Fatalf("Stat(%s): %v", s.Path, err)
		}
		if s.Size != fi.Size() || !s.Modified.Equal(fi.ModTime()) {
			t.Fatalf("ListStat(%s) = (size=%d, mod=%v), want (size=%d, mod=%v)", s.Path, s.Size, s.Modified, fi.Size(), fi.ModTime())
		}
	}
	if _, ok := byPath["Alpha.md"]; !ok {
		t.Fatal("ListStat missed Alpha.md")
	}
	if _, ok := byPath["Folder/Nested.arcmd"]; !ok {
		t.Fatal("ListStat missed Folder/Nested.arcmd")
	}
	if _, ok := byPath["Escape.md"]; ok {
		t.Fatal("ListStat exposed outside symlink")
	}
	if hasLinks {
		if _, ok := byPath["Alias.md"]; !ok {
			t.Fatal("ListStat missed internal symlink Alias.md")
		}
	}

	nodes, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	alphaNode := Find(nodes, "Alpha.md")
	nestedNode := Find(nodes, "Folder/Nested.arcmd")
	folderNode := Find(nodes, "Folder")
	emptyNode := Find(nodes, "Empty")
	if alphaNode == nil || nestedNode == nil || folderNode == nil || emptyNode == nil {
		t.Fatalf("Tree missing expected nodes: %+v", nodes)
	}
	if alphaNode.Modified.IsZero() || nestedNode.Modified.IsZero() || folderNode.Modified.IsZero() || emptyNode.Modified.IsZero() {
		t.Fatalf("Tree left zero Modified times: alpha=%v nested=%v folder=%v empty=%v",
			alphaNode.Modified, nestedNode.Modified, folderNode.Modified, emptyNode.Modified)
	}
	if folderNode.Modified.Before(nestedNode.Modified) {
		t.Fatalf("folder Modified %v older than child %v", folderNode.Modified, nestedNode.Modified)
	}
}
