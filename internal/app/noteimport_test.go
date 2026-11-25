package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportGuideCopiesOnlyAfterExplicitAction(t *testing.T) {
	a, tt := newTestApp(t)
	src := t.TempDir()
	if e := os.WriteFile(filepath.Join(src, "note.md"), []byte("# Imported\n"), 0600); e != nil {
		t.Fatal(e)
	}
	a.imports.open = true
	a.imports.source = src
	a.imports.spaceID = a.state.ActiveSpace().ID
	tt.Frame()
	if a.imports.result != nil {
		t.Fatal("opening guide imported content")
	}
	if e := tt.Click("复制并导入"); e != nil {
		t.Fatal(e)
	}
	tt.Frame()
	if a.imports.result == nil || a.imports.result.Documents != 1 || a.imports.err != "" {
		t.Fatalf("import result: %+v", a.imports)
	}
	if _, e := os.Stat(filepath.Join(src, "note.md")); e != nil {
		t.Fatal("original removed")
	}
}
