package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateSystemDir(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, ".arcmd", "history", "Note.md")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSystemDir(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, SystemDir, "history", "Note.md")); err != nil {
		t.Fatal("history not moved", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".arcmd")); !os.IsNotExist(err) {
		t.Fatal("legacy folder left", err)
	}
	// Both present: nothing moves, nothing is lost.
	os.MkdirAll(filepath.Join(root, ".arcmd", "templates"), 0o755)
	if err := MigrateSystemDir(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".arcmd", "templates")); err != nil {
		t.Fatal("legacy folder touched", err)
	}
	// Neither present, or a missing root, is not an error.
	if err := MigrateSystemDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSystemDir(""); err != nil {
		t.Fatal(err)
	}
}
