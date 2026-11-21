package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The tree takes each file's time from the folder listing rather than
// asking for the file again: it must be the time the file itself reports,
// a link's being its target's, and a folder's the newest it holds.
func TestTreeModifiedTimes(t *testing.T) {
	root := t.TempDir()
	old := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	newer := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	for rel, at := range map[string]time.Time{"a.md": old, "dir/b.md": newer, "dir/sub/c.md": old} {
		if err := Write(root, rel, "x"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(rel)), at, at); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]time.Time{"a.md": old, "dir/b.md": newer, "dir/sub/c.md": old, "dir": newer, "dir/sub": old} {
		n := Find(nodes, rel)
		if n == nil || !n.Modified.Equal(want) {
			t.Errorf("%s modified %v, want %v", rel, n, want)
		}
	}
	if err := os.Symlink(filepath.Join(root, "dir", "b.md"), filepath.Join(root, "link.md")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	nodes, err = Tree(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := Find(nodes, "link.md"); n == nil || !n.Modified.Equal(newer) {
		t.Errorf("a link's time is %v, want its target's %v", n, newer)
	}
}

// BenchmarkTree reads a space of 2,000 documents in 96 folders, as the
// sidebar does at launch and after every change to the folder.
func BenchmarkTree(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 2000; i++ {
		rel := fmt.Sprintf("project %02d/topic %02d/note %04d.md", i%12, i%8, i)
		if err := Write(root, rel, "x"); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodes, err := Tree(root)
		if err != nil || len(nodes) != 12 {
			b.Fatal(len(nodes), err)
		}
	}
}
