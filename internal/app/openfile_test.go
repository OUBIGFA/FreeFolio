package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemOpenUsesDeepestSpace(t *testing.T) {
	a, _ := newTestApp(t)
	nested := a.state.Add("Journal", filepath.Join(root(a), "Journal"))
	count := len(a.state.Spaces)
	if !a.openSystemFile(filepath.Join(nested.Root, "Ideas.md")) {
		t.Fatal("open")
	}
	if a.doc.space != nested || a.doc.rel != "Ideas.md" || len(a.state.Spaces) != count {
		t.Fatal("wrong space")
	}
}

func TestSystemOpenExternalMarkdownPreservesSource(t *testing.T) {
	a, _ := newTestApp(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "External.md")
	text := "# External\n\nUntouched Markdown.\n"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(file) || a.doc.rel != "External.md" {
		t.Fatal("open")
	}
	count := len(a.state.Spaces)
	if !a.openSystemFile(file) || len(a.state.Spaces) != count {
		t.Fatal("duplicate space")
	}
	if data, _ := os.ReadFile(file); string(data) != text {
		t.Fatal("source changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "External.arcmd")); !os.IsNotExist(err) {
		t.Fatal("opening converted Markdown")
	}
	if a.openSystemFile(dir) || a.openSystemFile(file+".missing") {
		t.Fatal("invalid input accepted")
	}
}

func TestSystemOpenRespectsUnsavedConflict(t *testing.T) {
	a, _ := newTestApp(t)
	old := a.doc
	a.doc.ed.ReplaceText(nil, "unsaved")
	a.doc.conflict = true
	count := len(a.state.Spaces)
	dir := t.TempDir()
	file := filepath.Join(dir, "External.md")
	if err := os.WriteFile(file, []byte("# Test"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.openSystemFile(file) || a.doc != old || len(a.state.Spaces) != count {
		t.Fatal("conflict was bypassed")
	}
}

func TestForwardedArgumentsKeepFirstFile(t *testing.T) {
	dir := t.TempDir()
	absolute := filepath.Join(dir, "Absolute.arcmd")
	files := documentArguments([]string{"First.md", "--flag", absolute}, dir)
	if len(files) != 2 || files[0] != filepath.Join(dir, "First.md") || files[1] != absolute {
		t.Fatal(files)
	}
	if files := documentArguments(nil, dir); len(files) != 0 {
		t.Fatal(files)
	}
}

func TestLooseFileNeverRegistersInWorkspace(t *testing.T) {
	a, tt := newTestApp(t)
	path := filepath.Join(a.cfg.ConfigDir, "spaces.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file := externalFile(t, "Loose.md", "# Loose\n")
	if !a.openSystemFile(file) {
		t.Fatal("open failed")
	}
	tt.Frame()
	if a.files == nil || !tt.HasText("Loose.md") || !tt.HasText("返回空间") {
		t.Fatal("file mode is missing")
	}
	a.doc.ed.ReplaceText(nil, "Edited ")
	if !a.flush() {
		t.Fatal("flush")
	}
	a.saveState()
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("opening an individual file changed the workspace registry")
	}
}

func TestSaveAsRescuesConflictAndRoutesDestination(t *testing.T) {
	a, _ := newTestApp(t)
	original := externalFile(t, "original.md", "# Original\\n")
	if !a.openSystemFile(original) {
		t.Fatal("open")
	}
	a.doc.ed.SetSelection(a.doc.ed.Len(), a.doc.ed.Len())
	a.doc.ed.ReplaceText(nil, " rescue")
	if err := os.WriteFile(original, []byte("Changed elsewhere"), 0600); err != nil {
		t.Fatal(err)
	}
	a.checkDisk()
	if !a.doc.conflict {
		t.Fatal("expected conflict")
	}
	dest := filepath.Join(root(a), "Recovered.md")
	// The copy, even one inside a space, takes the original's tab.
	if !a.saveAs(dest) || a.files == nil || a.doc.fullPath() != dest || len(a.files.order) != 1 || a.files.order[0] != a.doc.rel {
		t.Fatal("did not continue in the saved copy's tab")
	}
	b, err := os.ReadFile(dest)
	if err != nil || !bytes.Contains(b, []byte("rescue")) {
		t.Fatal("destination lost edits", err)
	}
	b, err = os.ReadFile(original)
	if err != nil || string(b) != "Changed elsewhere" {
		t.Fatal("original overwritten", err)
	}
}

func TestSaveAsKeepsUneditedBytesAndSplit(t *testing.T) {
	a, _ := newTestApp(t)
	a.addSplitPane()
	group, previous := a.split, a.doc
	original := externalFile(t, "raw.md", "---\\ntitle: Raw\\n---\\n\\n# Title\\n\\n")
	if !a.openSystemFile(original) {
		t.Fatal("open")
	}
	dest := filepath.Join(t.TempDir(), "copy.md")
	if !a.saveAs(dest) || a.doc.fullPath() != dest {
		t.Fatal("save as")
	}
	b, err := os.ReadFile(dest)
	want, _ := os.ReadFile(original)
	if err != nil || !bytes.Equal(b, want) {
		t.Fatal("save as rewrote an unedited document")
	}
	if !a.leaveFileMode() || a.doc != previous || a.split != group {
		t.Fatal("lost workspace split")
	}
}
