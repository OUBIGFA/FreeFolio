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
