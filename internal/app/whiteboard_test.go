package app

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/OUBIGFA/FreeMD/internal/whiteboard"
)

func TestWhiteboardCreateAutosaveAndExternalConflict(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.newWhiteboard("")
	tt.Frame()
	if a.doc == nil || a.doc.board == nil || !vault.IsWhiteboard(a.doc.rel) {
		t.Fatal("whiteboard document not created")
	}
	d := a.doc
	d.board.Scene.Elements = append(d.board.Scene.Elements, whiteboard.NewElement(whiteboard.KindRectangle, 10, 20, 100, 70))
	a.boardChanged(d)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	text, err := readFile(d.space, d.rel)
	if err != nil {
		t.Fatal(err)
	}
	s, err := whiteboard.Decode(text)
	if err != nil || len(s.Elements) != 1 {
		t.Fatalf("save: %v %s", err, text)
	}
	d.board.Scene.Elements[0].X = 55
	a.boardChanged(d)
	if err := os.WriteFile(d.fullPath(), []byte(text+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.save(); err != errConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	got, _ := os.ReadFile(d.fullPath())
	if string(got) != text+"\n" {
		t.Fatal("external modification overwritten")
	}
}

func TestWhiteboardReadErrorNeverOverwritesSource(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	bad := `{"type":"excalidraw","version":99,"elements":[]}`
	if err := vault.Write(root(a), "future.excalidraw", bad); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("future.excalidraw")
	tt.Frame()
	if a.doc.loadErr == nil {
		t.Fatal("future format accepted")
	}
	if err := a.doc.save(); err == nil {
		t.Fatal("bad file saved")
	}
	raw, _ := os.ReadFile(a.doc.fullPath())
	if string(raw) != bad {
		t.Fatal("bad file replaced")
	}
}

func TestWhiteboardSearchReadsSemanticText(t *testing.T) {
	s := whiteboard.NewScene()
	e := whiteboard.NewElement(whiteboard.KindText, 0, 0, 120, 30)
	e.Text = "[[接口文档]] #设计"
	e.OriginalText = e.Text
	s.Elements = append(s.Elements, e)
	raw, _ := s.Encode()
	x, err := extractText("architecture.excalidraw", raw)
	if err != nil || !strings.Contains(x.Text, "接口文档") || strings.Contains(x.Text, "strokeWidth") {
		t.Fatalf("semantic search: %+v %v", x, err)
	}
}

func TestWhiteboardKnowledgeCitationsAndRename(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.newWhiteboard("")
	tt.Frame()
	d := a.doc
	s := d.board.Scene
	e := whiteboard.NewElement(whiteboard.KindText, 20, 20, 200, 30)
	e.Text = "[[接口文档]] #设计"
	e.OriginalText = e.Text
	s.Elements = append(s.Elements, e)
	a.boardChanged(d)
	fs := a.qaSpace(d.space.ID)
	page, err := fs.Page(d.rel)
	if err != nil || !strings.Contains(strings.Join(page.Lines, "\n"), "接口文档") {
		t.Fatalf("QA page: %+v %v", page, err)
	}
	a.openCitation(d.space.ID, agent.Citation{Path: d.rel, From: 1, To: 1})
	if !d.board.Selected[e.ID] {
		t.Fatal("citation did not locate board element")
	}
	a.relink("接口文档", "接口规范", map[string]bool{d.space.ID: true})
	if !strings.Contains(d.board.Scene.PlainText(), "接口规范") {
		t.Fatal("board link was not renamed")
	}
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	a.openVersions()
	if len(a.versions.versions) == 0 {
		t.Fatal("board history absent")
	}
	a.selectVersion(0)
	if a.versions.board == nil {
		t.Fatal("board history not displayed as board")
	}
}

func TestMarkdownWhiteboardInsertionRetainsParentAndSharedWriter(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	parent := a.doc
	before := parent.text()
	a.insertWhiteboard(parent)
	tt.Frame()
	if parent.boardOverlay == nil || parent.boardOverlay.board == nil {
		t.Fatal("inline board not opened")
	}
	child := parent.boardOverlay
	if !strings.HasSuffix(child.rel, ".excalidraw.svg") {
		t.Fatal("Markdown board must be editable SVG")
	}
	if !strings.Contains(parent.text(), ".excalidraw.svg") || !strings.Contains(parent.text(), strings.Split(before, "\n")[0]) {
		t.Fatal("document contents lost")
	}
	child.board.Scene.Elements = append(child.board.Scene.Elements, whiteboard.NewElement(whiteboard.KindEllipse, 20, 40, 100, 80))
	a.boardChanged(child)
	if err := child.save(); err != nil {
		t.Fatal(err)
	}
	a.closeBoardOverlay(parent)
	if a.doc != parent || parent.boardOverlay != nil {
		t.Fatal("return lost parent document")
	}
	a.openBoardEmbed(parent, docRelativeURL(path.Dir(parent.rel), child.rel))
	if parent.boardOverlay == nil || len(parent.boardOverlay.board.Scene.Elements) != 1 {
		t.Fatal("inline edit not saved")
	}
}

func TestWhiteboardMoveAndTagUndoPreserveScene(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.newWhiteboard("")
	tt.Frame()
	d := a.doc
	e := whiteboard.NewElement(whiteboard.KindText, 20, 20, 150, 30)
	e.Text = "#old [[Note]]"
	e.OriginalText = e.Text
	d.board.Scene.Elements = append(d.board.Scene.Elements, e)
	a.boardChanged(d)
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root(a), "dest"), 0755); err != nil {
		t.Fatal(err)
	}
	to, _, err := a.moveWithResources(d.space, d.rel, d.space, "dest")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := vault.Read(root(a), to)
	if err != nil {
		t.Fatal(err)
	}
	scene, err := whiteboard.Decode(raw)
	if err != nil || scene.ElementByID(e.ID) == nil {
		t.Fatalf("move damaged board: %v", err)
	}
}

// The home page, shown while no document is open, starts a whiteboard as
// it starts a document.
func TestHomeNewWhiteboardButton(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	if a.doc != nil {
		a.closeOpen(a.doc.space, a.doc.rel)
		tt.Frame()
	}
	if a.doc != nil {
		t.Fatal("home page needs no open document")
	}
	// The title bar's button of the same name shows only with a document:
	// the one found is the home page's, below the title bar.
	r, ok := tt.Find("新建白板")
	if !ok || r.Y < 100 {
		t.Fatalf("home whiteboard button %+v, found %v", r, ok)
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if a.doc == nil || a.doc.board == nil || !vault.IsWhiteboard(a.doc.rel) {
		t.Fatal("home whiteboard button did not open a new whiteboard")
	}
}

// The title bar starts a whiteboard while a document shows.
func TestTitleBarNewWhiteboardButton(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	if a.doc == nil {
		t.Fatal("the test needs an open document, so the home page is hidden")
	}
	before := a.doc.rel
	if err := tt.Click("新建白板"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc == nil || a.doc.board == nil || !vault.IsWhiteboard(a.doc.rel) || a.doc.rel == before {
		t.Fatal("title bar whiteboard button did not open a new whiteboard")
	}
}
