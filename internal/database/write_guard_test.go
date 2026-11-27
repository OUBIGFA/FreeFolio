package database

import (
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"testing"
)

func TestFlushRecordRejectsExternalReplacement(t *testing.T) {
	root := t.TempDir()
	initial := "---\nstatus: 未开始\n---\n\n# Entry\n\nOriginal body\n"
	if err := vault.Write(root, "Tasks/Entry.md", initial); err != nil {
		t.Fatal(err)
	}
	s := NewStore(root, "Tasks.db.md", NewDefault("Tasks", SourceConfig{Type: SourceFolder}))
	if _, err := s.Refresh(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCell("Tasks/Entry.md", "prop_status", TextValue("已完成")); err != nil {
		t.Fatal(err)
	}
	external := "---\nstatus: 进行中\n---\n\n# External\n\nExternal body\n"
	if err := vault.Write(root, "Tasks/Entry.md", external); err != nil {
		t.Fatal(err)
	}
	if err := s.FlushRecord("Tasks/Entry.md"); err == nil {
		t.Fatal("stale property update overwrote an external edit")
	}
	got, err := vault.Read(root, "Tasks/Entry.md")
	if err != nil || got != external {
		t.Fatalf("external file changed: %q %v", got, err)
	}
}

func TestRestoreSchemaIsUndoable(t *testing.T) {
	before := NewDefault("Current", SourceConfig{Type: SourceInline})
	s := NewStore(t.TempDir(), "Tasks.db.md", before)
	older := before.Clone()
	older.Title = "Older"
	if err := s.RestoreSchema(older); err != nil {
		t.Fatal(err)
	}
	if s.Database().Title != "Older" {
		t.Fatal("version was not restored")
	}
	if _, ok := s.Undo(); !ok || s.Database().Title != "Current" {
		t.Fatal("restore cannot be undone")
	}
	if _, ok := s.Redo(); !ok || s.Database().Title != "Older" {
		t.Fatal("restore cannot be redone")
	}
}
