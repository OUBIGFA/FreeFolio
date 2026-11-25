package prefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdoptionPreferencesMigrateWithoutEnablingQuickCapture(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if e := os.WriteFile(s.Path, []byte(`{"version":16,"font":"serif","fontSize":21,"autoSpace":true,"keys":{"save":"Ctrl+S"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := s.Load()
	if e != nil {
		t.Fatal(e)
	}
	if p.Version != CurrentVersion || p.Font != "serif" || p.FontSize != 21 || !p.AutoSpace || p.QuickNoteEnabled || p.BodyFont != "" || p.Keys["save"] != "Ctrl+S" {
		t.Fatalf("migration: %+v", p)
	}
	p.BodyFont = "Georgia"
	p.InterfaceFont = "Arial"
	p.FavoriteFonts = []string{"Georgia"}
	p.RecentBodyFonts = []string{"Georgia"}
	p.QuickNoteShortcut = "Ctrl+Shift+Space"
	p.QuickNoteSpace = "personal"
	p.QuickNoteFolder = "Inbox"
	if e = s.Save(p); e != nil {
		t.Fatal(e)
	}
	q, e := s.Load()
	if e != nil || q.BodyFamily() != "Georgia" || q.InterfaceFont != "Arial" || len(q.FavoriteFonts) != 1 || q.QuickNoteFolder != "Inbox" {
		t.Fatalf("roundtrip: %+v %v", q, e)
	}
}
