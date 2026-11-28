package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func TestSpaceSwitchReportsSettingsFailureAfterBackgroundSave(t *testing.T) {
	a, _ := newTestApp(t)
	other := a.state.Add("Other", t.TempDir())
	a.store = space.NewStore(t.TempDir()) // A directory cannot be replaced by JSON.
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	a.notices = nil
	if !a.switchSpaceOnly(other.ID) {
		t.Fatal("space did not switch")
	}
	if len(a.notices) != 0 {
		t.Fatal("switch waited for the settings write before returning")
	}
	awaitTreeUpdate(t, updates)()
	if len(a.notices) == 0 {
		t.Fatal("failed background save was not reported")
	}
}

func TestResumedWhiteboardChecksDiskInBackground(t *testing.T) {
	a, _ := newTestApp(t)
	a.newWhiteboard("")
	old := a.doc
	a.doc = nil
	a.session(old.space.ID).doc = old
	scene := whiteboard.NewScene()
	scene.Elements = append(scene.Elements, whiteboard.NewElement(whiteboard.KindRectangle, 0, 0, 80, 50))
	raw, err := scene.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(old.space.Root, old.rel, raw); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	a.resumeSpace()
	if a.doc != old {
		t.Fatal("resuming reread the board before showing its resident scene")
	}
	awaitTreeUpdate(t, updates)()
	if a.doc == old || a.doc.board == nil || len(a.doc.board.Scene.Elements) != 1 {
		t.Fatal("background check did not apply the externally changed board")
	}
	if a.doc.dirty() {
		t.Fatal("external reload changed the saved document")
	}
}

func TestSwitchSettingsSaveCannotOverwriteLaterSave(t *testing.T) {
	a, _ := newTestApp(t)
	other := a.state.Add("Other", t.TempDir())
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	a.switchSpaceOnly(other.ID)
	other.Name = "Renamed"
	a.saveState()
	loaded, err := space.NewStore(filepath.Join(a.cfg.ConfigDir, "spaces.json")).Load()
	if err != nil || loaded.Get(other.ID).Name != "Renamed" || loaded.Active != other.ID {
		t.Fatalf("later save lost state: %v", err)
	}
}

func TestResumedWhiteboardPreservesEditsDuringDiskCheck(t *testing.T) {
	a, _ := newTestApp(t)
	a.newWhiteboard("")
	d := a.doc
	a.doc = nil
	a.session(d.space.ID).doc = d
	external := d.board.Scene.Clone()
	external.Elements = append(external.Elements, whiteboard.NewElement(whiteboard.KindEllipse, 0, 0, 90, 90))
	raw, err := external.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(d.space.Root, d.rel, raw); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	a.resumeSpace()
	apply := awaitTreeUpdate(t, updates)
	d.board.Scene.Elements = append(d.board.Scene.Elements, whiteboard.NewElement(whiteboard.KindRectangle, 0, 0, 50, 50))
	a.boardChanged(d)
	apply()
	if a.doc != d || !d.conflict || d.diskNow != raw || len(d.board.Scene.Elements) != 1 || d.board.Scene.Elements[0].Kind != whiteboard.KindRectangle {
		t.Fatal("background check overwrote a live edit instead of exposing a conflict")
	}
}

func TestResumedWhiteboardMissingFileIsNotRecreated(t *testing.T) {
	a, _ := newTestApp(t)
	a.newWhiteboard("")
	d := a.doc
	a.doc = nil
	a.session(d.space.ID).doc = d
	if err := os.Remove(filepath.Join(d.space.Root, d.rel)); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 8)
	a.runAsync = func(fn func()) { updates <- fn }
	a.resumeSpace()
	awaitTreeUpdate(t, updates)()
	if a.doc != nil || a.state.ActiveSpace().LastOpen == d.rel {
		t.Fatal("missing board stayed open after the disk check")
	}
	if _, err := os.Stat(filepath.Join(d.space.Root, d.rel)); !os.IsNotExist(err) {
		t.Fatal("resuming recreated a deleted board")
	}
}
