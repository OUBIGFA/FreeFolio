package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

func TestCleanupRescansAndKeepsOriginalOnRecycleFailure(t *testing.T) {
	a, _ := newTestApp(t)
	if e := os.MkdirAll(filepath.Join(root(a), "assets"), 0755); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root(a), "assets", "unused.png")
	if e := os.WriteFile(p, []byte("image"), 0600); e != nil {
		t.Fatal(e)
	}
	a.scanUnusedAssets()
	if len(a.cleanup.items) != 1 {
		t.Fatalf("scan: %+v", a.cleanup)
	}
	if e := vault.Write(root(a), "new-reference.md", "![](assets/unused.png)"); e != nil {
		t.Fatal(e)
	}
	called := false
	a.trashFile = func(string) error { called = true; return errors.New("recycle unavailable") }
	a.recycleUnusedAssets()
	if called {
		t.Fatal("newly referenced attachment recycled")
	}
	if e := vault.Write(root(a), "new-reference.md", "no image"); e != nil {
		t.Fatal(e)
	}
	a.scanUnusedAssets()
	a.recycleUnusedAssets()
	if !called || a.cleanup.err == "" {
		t.Fatal("recycle error hidden")
	}
	if _, e := os.Stat(p); e != nil {
		t.Fatal("failed recycle removed original")
	}
	entries, e := os.ReadDir(a.bin.Dir)
	if e != nil || len(entries) == 0 {
		t.Fatal("no recovery copy before recycle")
	}
}

func TestCleanupViewSelectAllToggle(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1100, 760)
	if e := os.MkdirAll(filepath.Join(root(a), "assets"), 0755); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"one.png", "two.png"} {
		if e := os.WriteFile(filepath.Join(root(a), "assets", name), []byte("image-bytes"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	a.openSettings(tabSpaces)
	a.scanUnusedAssets()
	tt.Frame()
	if !tt.HasText("全选（已选 2 / 2 项）") {
		t.Fatalf("expected select-all summary, got %q", tt.Texts())
	}
	if err := tt.Click("全选（已选 2 / 2 项）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("全选（已选 0 / 2 项）") {
		t.Fatalf("select-all did not uncheck all: %q", tt.Texts())
	}
	if err := tt.Click("全选（已选 0 / 2 项）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("全选（已选 2 / 2 项）") {
		t.Fatalf("select-all did not re-check all: %q", tt.Texts())
	}
}
