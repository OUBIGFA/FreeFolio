package prefs

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "sub", "prefs.json")}
	p, err := s.Load()
	if err != nil || p.Typewriter || p.Version != CurrentVersion {
		t.Fatalf("defaults %+v %v", p, err)
	}
	p.Typewriter = true
	p.Keys = map[string]string{"palette": "Cmd+J"}
	p.TourSeen = true
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := s.Load()
	if err != nil || !q.Typewriter || q.Keys["palette"] != "Cmd+J" || !q.TourSeen {
		t.Fatalf("loaded %+v %v", q, err)
	}
	os.WriteFile(s.Path, []byte(fmt.Sprintf(`{"version": %d}`, CurrentVersion+1)), 0o644)
	if _, err := s.Load(); err == nil {
		t.Error("newer version accepted")
	}
}

func TestGrayFolderIconsMigrateAndRoundTrip(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(store.Path, []byte(`{"version":8,"sidebarLeft":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil || p.Version != CurrentVersion || !p.SidebarLeft || p.GrayFolderIcons {
		t.Fatalf("version 8 migrated: %+v, %v", p, err)
	}
	p.GrayFolderIcons = true
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load()
	if err != nil || !again.GrayFolderIcons {
		t.Fatalf("gray folder icons round-trip: %+v, %v", again, err)
	}
}

func TestReadingPreferencesDefaultsAndRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p, _ := s.Load()
	if p.ReadingSize() != 16 || p.ReadingLine() != 1.625 {
		t.Fatal("reading defaults")
	}
	p.FontSize, p.LineHeight, p.NoAutoPair = 22, 1.9, true
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	q, err := s.Load()
	if err != nil || q.FontSize != 22 || q.LineHeight != 1.9 || !q.NoAutoPair {
		t.Fatal(q, err)
	}
	os.WriteFile(s.Path, []byte(`{"version":1,"fontSize":9999,"lineHeight":-2}`), 0600)
	q, err = s.Load()
	if err != nil || q.ReadingSize() != 16 || q.ReadingLine() != 1.625 {
		t.Fatal("invalid reading values", q, err)
	}
}

func TestFontAndPageWidth(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p := &Prefs{Font: "serif", Page: "wide"}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got.BodyFamily() == "" || got.Column() != 1040 {
		t.Fatal(got, err)
	}
	got.Font, got.Page = "comic", "huge"
	if err := store.Save(got); err != nil {
		t.Fatal(err)
	}
	if got.Font != "" || got.Page != "" || got.BodyFamily() != "" || got.Column() != 0 {
		t.Fatal("unknown choices fall back to the defaults")
	}
}

func TestSidebarMigration(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(store.Path, []byte(`{"version":1,"typewriter":true,"font":"serif"}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil || p.Version != CurrentVersion || p.SidebarLeft || !p.Typewriter || p.Font != "serif" {
		t.Fatalf("migrated preferences: %+v, %v", p, err)
	}
}

func TestFilesPrefsMigrateAndRoundTrip(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(store.Path, []byte(`{"version":2,"sidebarLeft":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil || p.Version != CurrentVersion || !p.SidebarLeft || p.Files != (Files{}) {
		t.Fatalf("version 2 migrated: %+v, %v", p, err)
	}
	p.Files = Files{KeepOpen: true, NoDoubleClickClose: true, HideTabClose: true, TabAfterCurrent: true, ReadOnly: true}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load()
	if err != nil || again.Files != p.Files {
		t.Fatalf("file browser choices: %+v, %v", again, err)
	}
}

func TestKeyStrategyMigratesAndRoundTrips(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	old := `{"version":9,"ai":{"active":"a","connections":[{"id":"a","name":"A","protocol":"chat","baseURL":"https://x.example/v1"}]}}`
	if err := os.WriteFile(store.Path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil || p.Version != CurrentVersion || p.AI.Current() == nil || p.AI.Current().KeyStrategy != "" {
		t.Fatalf("version 9 migrated: %+v, %v", p, err)
	}
	p.AI.Current().KeyStrategy = "failover"
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load()
	if err != nil || again.AI.Current().KeyStrategy != "failover" {
		t.Fatalf("key strategy round-trip: %+v, %v", again, err)
	}
}
