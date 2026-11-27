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
