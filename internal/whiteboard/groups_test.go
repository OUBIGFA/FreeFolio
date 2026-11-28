package whiteboard

import (
	"strings"
	"testing"
)

func TestGroupNameSurvivesSaveUndoAndClipboard(t *testing.T) {
	s := NewScene()
	a := NewElement(KindRectangle, 0, 0, 100, 80)
	b := NewElement(KindRectangle, 200, 0, 100, 80)
	s.Elements = []*Element{a, b}
	id := s.Group([]string{a.ID, b.ID})
	h := NewHistory()
	h.Begin(s)
	if !s.RenameGroup(id, "Image workflow") || !h.Commit(s) {
		t.Fatal("group rename was not recorded")
	}
	raw, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Decode(raw)
	if err != nil || loaded.GroupNames[id] != "Image workflow" {
		t.Fatalf("group name did not persist: %v", err)
	}
	undone, ok := h.Undo(s)
	if !ok || undone.GroupNames[id] != "" {
		t.Fatal("undo did not restore the unnamed group")
	}
	redone, ok := h.Redo(undone)
	if !ok || redone.GroupNames[id] != "Image workflow" {
		t.Fatal("redo lost the name")
	}
	clip, err := s.Clipboard([]string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := loaded.Paste(clip, Point{500, 500})
	if err != nil {
		t.Fatal(err)
	}
	copyID := loaded.ElementByID(ids[0]).GroupIDs[0]
	if copyID == id || loaded.GroupNames[copyID] != "Image workflow" {
		t.Fatal("pasted group did not receive an independent named identity")
	}
	loaded.Ungroup(ids)
	if _, ok := loaded.GroupNames[copyID]; ok {
		t.Fatal("ungroup kept the removed group name")
	}
}

func TestGroupRegionsFollowMembersAndProtectLockedNames(t *testing.T) {
	s := NewScene()
	a := NewElement(KindRectangle, 100, 100, 100, 80)
	b := NewElement(KindRectangle, 300, 100, 100, 80)
	s.Elements = []*Element{a, b}
	id := s.Group([]string{a.ID, b.ID})
	r := s.GroupRegions()
	if len(r) != 1 || r[0].Bounds != (Box{76, 48, 348, 156}) {
		t.Fatalf("group padding = %+v", r)
	}
	s.Translate([]string{a.ID, b.ID}, Point{30, 40})
	if got := s.GroupRegions()[0].Bounds; got != (Box{106, 88, 348, 156}) {
		t.Fatalf("group did not follow members: %v", got)
	}
	b.Locked = true
	if s.RenameGroup(id, "Blocked") {
		t.Fatal("renamed a group containing a locked member")
	}
	b.Locked = false
	if !s.RenameGroup(id, "Design <A>") {
		t.Fatal("could not rename unlocked group")
	}
	svg, err := ExportSVG(s, SVGOptions{})
	if err != nil || !strings.Contains(svg, "Design &lt;A&gt;") || !strings.Contains(svg, `fill-opacity="0.12"`) {
		t.Fatalf("export lost group appearance: %v", err)
	}
}

func TestNestedGroupHeadersHaveSeparateSpace(t *testing.T) {
	s := NewScene()
	a := NewElement(KindRectangle, 100, 100, 100, 80)
	b := NewElement(KindRectangle, 300, 100, 100, 80)
	s.Elements = []*Element{a, b}
	inner := s.Group([]string{a.ID, b.ID})
	outer := s.Group([]string{a.ID, b.ID})
	regions := s.GroupRegions()
	if len(regions) != 2 || regions[0].ID != outer || regions[1].ID != inner || regions[0].Bounds.Y+28 >= regions[1].Bounds.Y {
		t.Fatalf("nested titles overlap: %+v", regions)
	}
}
