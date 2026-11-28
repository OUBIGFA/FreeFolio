package gitsync

import (
	"testing"
	"time"
)

func TestScheduleDoesNotReplayEnableAndSupportsSaveDelay(t *testing.T) {
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.Local)
	c := DefaultSpace()
	var s Schedule
	s.Configure(c, now, true)
	c.Enabled, c.Automatic, c.OnStart = true, true, true
	s.Configure(c, now, false)
	if s.Ready(now) || s.Ready(now.Add(time.Minute)) {
		t.Fatal("enable replayed startup")
	}
	c.OnSave, c.SaveDelaySeconds = true, 40
	s.Configure(c, now, false)
	s.Saved(now)
	if s.Ready(now.Add(39*time.Second)) || !s.Ready(now.Add(40*time.Second)) {
		t.Fatal("save delay ignored")
	}
	c.Automatic = false
	s.Configure(c, now.Add(time.Minute), false)
	c.Automatic = true
	s.Configure(c, now.Add(2*time.Minute), false)
	if s.Ready(now.Add(2 * time.Minute)) {
		t.Fatal("disabled pending task replayed")
	}
}

func TestSaveScheduleWaitsAfterLastSave(t *testing.T) {
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.Local)
	c := DefaultSpace()
	c.Enabled, c.Automatic, c.OnSave = true, true, true
	c.SaveDelaySeconds = 30
	var s Schedule
	s.Configure(c, now, false)
	s.Saved(now)
	s.Saved(now.Add(20 * time.Second))
	if s.Ready(now.Add(31*time.Second)) || !s.Ready(now.Add(50*time.Second)) {
		t.Fatal("save delay was not debounced")
	}
}

func TestSaveSyncOptInImmediateAndDisableClearsPending(t *testing.T) {
	now := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	c := DefaultSpace()
	if c.OnSave {
		t.Fatal("save sync must default to off")
	}
	c.Enabled, c.Automatic = true, true
	c.Binding = Binding{Remote: "origin", URL: "https://github.com/example/notes.git", Branch: "main"}
	c.SaveDelaySeconds = 0
	if err := c.Validate(); err != nil {
		t.Fatalf("zero seconds must allow immediate sync: %v", err)
	}
	var s Schedule
	s.Configure(c, now, false)
	planned := s.Next
	s.Saved(now)
	if s.Next != planned || s.Ready(now) {
		t.Fatal("save triggered while opt-in is off")
	}
	c.OnSave = true
	s.Configure(c, now, false)
	if s.Ready(now) {
		t.Fatal("enabling replayed a previous save")
	}
	s.Saved(now)
	if !s.Ready(now) {
		t.Fatal("zero delay did not trigger on save")
	}
	c.OnSave = false
	s.Configure(c, now, false)
	if s.Ready(now) || s.Next != planned || !s.Ready(planned) {
		t.Fatal("disabling failed to clear the saved job or changed periodic sync")
	}
	c.SaveDelaySeconds = -1
	if c.Validate() == nil {
		t.Fatal("negative delay accepted")
	}
	c.SaveDelaySeconds = 10
	c.OnSave = true
	s.Configure(c, now, false)
	s.Saved(now)
	s.Saved(now.Add(5 * time.Second))
	if s.Ready(now.Add(14*time.Second)) || !s.Ready(now.Add(15*time.Second)) {
		t.Fatal("delay did not follow latest save")
	}
	for _, enabled := range []bool{false, true} {
		c.OnSave = enabled
		dir := t.TempDir()
		if err := SaveConfig(dir, Config{Spaces: map[string]*SpaceConfig{"notes": c}}); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadConfig(dir)
		if err != nil || loaded.Spaces["notes"].OnSave != enabled || loaded.Spaces["notes"].SaveDelaySeconds != 10 {
			t.Fatalf("save preference lost: %+v %v", loaded, err)
		}
	}
}

func TestScheduleDailyLimitsAndRetry(t *testing.T) {
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.Local)
	c := DefaultSpace()
	c.Enabled = true
	c.Automatic = true
	c.Plan = "daily"
	c.DailyTime = "09:10"
	c.DailyLimit = 1
	var s Schedule
	s.Configure(c, now, true)
	if s.Next.Format("15:04") != "09:10" {
		t.Fatal(s.Next)
	}
	s.Finished(now.Add(10*time.Minute), false, true)
	if s.Ready(now.Add(11 * time.Minute)) {
		t.Fatal("daily limit ignored")
	}
	if !s.Ready(now.Add(24*time.Hour + 10*time.Minute)) {
		t.Fatal("daily limit did not reset")
	}
	c.DailyLimit = 0
	c.Plan = "interval"
	c.RetryLimit = 1
	s.Configure(c, now, false)
	s.Finished(now, false, true)
	if !s.Ready(now.Add(time.Minute)) {
		t.Fatal("retry not scheduled")
	}
	s.Finished(now.Add(time.Minute), false, true)
	if s.Ready(now.Add(2 * time.Minute)) {
		t.Fatal("retry limit ignored")
	}
}

func TestConfigRoundTripAndInvalidPlan(t *testing.T) {
	dir := t.TempDir()
	c := Config{Version: 1, Spaces: map[string]*SpaceConfig{"notes": DefaultSpace()}}
	c.Spaces["notes"].Binding = Binding{Remote: "origin", URL: "https://github.com/a/b.git", Branch: "main"}
	if err := SaveConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(dir)
	if err != nil || got.Spaces["notes"].Binding.Branch != "main" {
		t.Fatal(got, err)
	}
	got.Spaces["notes"].DailyTime = "25:00"
	if SaveConfig(dir, got) == nil {
		t.Fatal("invalid time accepted")
	}
}

func TestStructuredMergeRejectsInvalidWhiteboard(t *testing.T) {
	if validateMergedFile("scene.excalidraw", []byte(`{"type":"excalidraw","elements":`)) == nil {
		t.Fatal("invalid whiteboard accepted")
	}
	if e := validateMergedFile("note.md", []byte("ordinary note")); e != nil {
		t.Fatal(e)
	}
}
