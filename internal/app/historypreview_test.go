package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
)

func TestHistoryPreviewLeavesOriginalEditorAndDiskUntouched(t *testing.T) {
	a, tt := newTestApp(t)
	const current = "# Current\n\nCurrent paragraph.\n"
	if e := vault.Write(root(a), "Preview.md", current); e != nil {
		t.Fatal(e)
	}
	if e := history.Record(root(a), "Preview.md", "# Previous\n\nPrevious paragraph.\n", time.Now().Add(-time.Hour), true); e != nil {
		t.Fatal(e)
	}
	a.refreshTree()
	a.open("Preview.md")
	tt.Frame()
	original := a.doc.ed
	a.openVersions()
	tt.Frame()
	a.selectVersion(0)
	tt.Frame()
	if a.versions.preview == nil || !a.versions.preview.ReadOnly || a.versions.preview == original {
		t.Fatal("history must use its own read-only renderer")
	}
	if a.doc.ed != original || original.Committed().PlainText() != a.versions.for_.ed.Committed().PlainText() {
		t.Fatal("preview replaced the editing document")
	}
	before, _ := os.ReadFile(filepath.Join(root(a), "Preview.md"))
	if string(before) != current {
		t.Fatalf("preview saved history: %q", before)
	}
	if e := tt.Click("返回当前文档"); e != nil {
		t.Fatal(e)
	}
	tt.Frame()
	if a.versions.open || a.doc.ed != original {
		t.Fatal("return did not preserve the editor")
	}
}
