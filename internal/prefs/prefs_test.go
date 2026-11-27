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

func TestOptionalResourceDirectoryMigratesWithoutChangingOtherChoices(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(store.Path, []byte(`{"version":17,"sidebarLeft":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || loaded.Version != CurrentVersion || loaded.ResourceDir != "" || !loaded.SidebarLeft {
		t.Fatalf("migration: %+v %v", loaded, err)
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

func TestNoSpaceStarterMigratesAndRoundTrips(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(store.Path, []byte(`{"version":15,"sidebarLeft":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil || p.Version != CurrentVersion || !p.SidebarLeft || p.NoSpaceStarter {
		t.Fatalf("version 15 migrated: %+v, %v", p, err)
	}
	p.NoSpaceStarter = true
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load()
	if err != nil || !again.NoSpaceStarter {
		t.Fatalf("noSpaceStarter round-trip: %+v, %v", again, err)
	}
}

func TestComfyUIPrefsMigrateAndRoundTrip(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	if err := os.WriteFile(store.Path, []byte(`{"version":18,"resourceDir":"C:\\resources"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := store.Load()
	if err != nil || p.Version != CurrentVersion || p.ResourceDir != `C:\resources` || p.ComfyUI != (ComfyUIPrefs{}) {
		t.Fatalf("version 18 migrated: %+v, %v", p, err)
	}
	p.ComfyUI = ComfyUIPrefs{Dir: `D:\ComfyUI`, URL: "http://127.0.0.1:8188"}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	again, err := store.Load()
	if err != nil || again.ComfyUI != p.ComfyUI {
		t.Fatalf("ComfyUI prefs round-trip: %+v, %v", again, err)
	}
}

func TestPageZoomDefaultsStepAndRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.PageZoom != 0 || p.Zoom() != DefaultPageZoom {
		t.Fatalf("default zoom = (%v, %v); want (0, %v)", p.PageZoom, p.Zoom(), DefaultPageZoom)
	}

	if got := NormalizePageZoom(0); got != DefaultPageZoom {
		t.Fatalf("NormalizePageZoom(0) = %v; want 1", got)
	}
	if got := NormalizePageZoom(0.2); got != MinPageZoom {
		t.Fatalf("NormalizePageZoom(0.2) = %v; want %v", got, MinPageZoom)
	}
	if got := NormalizePageZoom(4.5); got != MaxPageZoom {
		t.Fatalf("NormalizePageZoom(4.5) = %v; want %v", got, MaxPageZoom)
	}

	if got := StepPageZoom(1.0, 1); got != 1.1 {
		t.Fatalf("StepPageZoom(1.0, +1) = %v; want 1.1", got)
	}
	if got := StepPageZoom(1.0, -1); got != 0.9 {
		t.Fatalf("StepPageZoom(1.0, -1) = %v; want 0.9", got)
	}
	if got := StepPageZoom(1.25, 1); got != 1.3 {
		t.Fatalf("StepPageZoom(1.25, +1) = %v; want 1.3", got)
	}
	if got := StepPageZoom(1.25, -1); got != 1.2 {
		t.Fatalf("StepPageZoom(1.25, -1) = %v; want 1.2", got)
	}
	if got := StepPageZoom(MaxPageZoom, 1); got != MaxPageZoom {
		t.Fatalf("StepPageZoom(max, +1) = %v; want %v", got, MaxPageZoom)
	}
	if got := StepPageZoom(MinPageZoom, -1); got != MinPageZoom {
		t.Fatalf("StepPageZoom(min, -1) = %v; want %v", got, MinPageZoom)
	}

	p.PageZoom = 1.3
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil || loaded.PageZoom != 1.3 || loaded.Zoom() != 1.3 {
		t.Fatalf("loaded = %+v, err=%v", loaded, err)
	}

	// Default 1.0 normalizes to 0 and is omitted from prefs.json.
	loaded.PageZoom = DefaultPageZoom
	if err := s.Save(loaded); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PageZoom != 0 || string(raw) == "" {
		t.Fatalf("default zoom did not normalize to 0: %+v", loaded)
	}
	if stringContains(string(raw), "pageZoom") {
		t.Fatalf("default 100%% zoom should be omitted from json: %s", raw)
	}

	// Out-of-range JSON values clamp cleanly on load.
	if err := os.WriteFile(s.Path, []byte(`{"version":1,"pageZoom":9.9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clamped, err := s.Load()
	if err != nil || clamped.Zoom() != MaxPageZoom {
		t.Fatalf("clamped high = %+v, err=%v", clamped, err)
	}
	if err := os.WriteFile(s.Path, []byte(`{"version":1,"pageZoom":0.1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clamped, err = s.Load()
	if err != nil || clamped.Zoom() != MinPageZoom {
		t.Fatalf("clamped low = %+v, err=%v", clamped, err)
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSidebarDensityRoundTrip(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "prefs.json")}
	p, err := s.Load()
	if err != nil || p.SidebarRowHeight() != 32 {
		t.Fatalf("default sidebar row height = %v, err=%v", p.SidebarRowHeight(), err)
	}
	for _, tc := range []struct {
		density string
		want    float32
	}{
		{"compact", 28},
		{"", 32},
		{"comfortable", 40},
	} {
		p.SidebarDensity = tc.density
		if err := s.Save(p); err != nil {
			t.Fatal(err)
		}
		got, err := s.Load()
		if err != nil || got.SidebarDensity != tc.density || got.SidebarRowHeight() != tc.want {
			t.Fatalf("density %q: got %+v height=%v err=%v", tc.density, got, got.SidebarRowHeight(), err)
		}
	}
	p.SidebarDensity = "giant"
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got.SidebarDensity != "" || got.SidebarRowHeight() != 32 {
		t.Fatalf("invalid density did not fall back: %+v err=%v", got, err)
	}
}
