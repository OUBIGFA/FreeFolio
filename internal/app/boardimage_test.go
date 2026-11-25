package app

import (
	"bytes"
	"encoding/base64"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func boardImageFixture(t *testing.T) (*App, *boardImageWindow, *fakeWindow, []byte) {
	t.Helper()
	a, tt := newTestApp(t)
	tt.Frame()
	a.newWhiteboard("")
	tt.Frame()
	d := a.doc
	img := image.NewRGBA(image.Rect(0, 0, 240, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 240; x++ {
			img.Set(x, y, color.RGBA{240, 245, 250, 255})
		}
	}
	var buf bytes.Buffer
	if e := png.Encode(&buf, img); e != nil {
		t.Fatal(e)
	}
	b := d.board
	source := whiteboard.NewAINode(whiteboard.AIImageInput, 20, 20)
	source.FileID = "original"
	b.Scene.Files[source.FileID] = &whiteboard.FileData{ID: source.FileID, MIMEType: "image/png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}
	b.Scene.Elements = append(b.Scene.Elements, source)
	var win *fakeWindow
	a.newWindow = func(title string, view func(*ui.Context), close func() bool) windowHandle {
		win = &fakeWindow{title: title, view: view, close: close}
		return win
	}
	a.openBoardImage(d, source.ID, true)
	if len(a.boardImages) != 1 || a.boardImages[0].image == nil {
		t.Fatalf("image not opened: %+v", a.boardImages)
	}
	return a, a.boardImages[0], win, buf.Bytes()
}

func TestBoardImageAnnotationBecomesGenerationReferenceWithoutChangingOriginal(t *testing.T) {
	a, w, _, original := boardImageFixture(t)
	main := a.doc
	scene := main.board
	originalElement := scene.Scene.ElementByID(w.nodeID).Clone()
	mark := whiteboard.NewElement(whiteboard.KindEllipse, 30, 30, 70, 50)
	mark.StrokeColor = "#e03131"
	mark.StrokeWidth = 4
	w.image.Board.Scene.Elements = append(w.image.Board.Scene.Elements, mark)
	a.saveBoardImage(w, true)
	if w.err != "" || w.message == "" {
		t.Fatalf("save: %s %s", w.err, w.message)
	}
	if a.doc != main || scene.Scene.ElementByID(w.nodeID).FileID != originalElement.FileID {
		t.Fatal("original node or main workspace changed")
	}
	if got := scene.Scene.Files[originalElement.FileID].DataURL; got != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(original) {
		t.Fatal("original bytes replaced")
	}
	var generated, reference *whiteboard.Element
	for _, e := range scene.Scene.Elements {
		n := whiteboard.AINodeOf(e)
		if e.ID == w.nodeID || n == nil {
			continue
		}
		if n.Role == whiteboard.AIImage {
			generated = e
		} else if n.Role == whiteboard.AIImageInput {
			reference = e
		}
	}
	if generated == nil || reference == nil {
		t.Fatal("new node pair missing")
	}
	inputs := scene.Scene.AIInputs(generated.ID)
	if len(inputs) != 1 || inputs[0].From != reference.ID {
		t.Fatal("new generator does not use annotated input")
	}
	f := scene.Scene.Files[reference.FileID]
	data, e := os.ReadFile(filepath.Join(root(a), filepath.FromSlash(f.Path)))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(data, original) {
		t.Fatal("annotation was not rendered")
	}
	if !scene.Undo() || len(scene.Scene.Elements) != 1 {
		t.Fatal("creation not undoable as one operation")
	}
}

func TestBoardImageLockedNodeCanViewButCannotSave(t *testing.T) {
	a, w, _, _ := boardImageFixture(t)
	source := w.doc.board.Scene.ElementByID(w.nodeID)
	source.Locked = true
	a.saveBoardImage(w, true)
	if w.err == "" || len(w.doc.board.Scene.Elements) != 1 {
		t.Fatal("locked node accepted edit")
	}
	w.savedRevision = w.image.Board.Revision()
	if !a.closeBoardImage(w) {
		t.Fatal("unchanged viewer did not close")
	}
	a.openBoardImage(a.doc, source.ID, false)
	if len(a.boardImages) != 1 || a.boardImages[0].editing {
		t.Fatal("locked node cannot be inspected")
	}
}

func TestBoardImageAnnotationSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect image annotations")
	a, w, win, _ := boardImageFixture(t)
	mark := whiteboard.NewElement(whiteboard.KindRectangle, 55, 40, 95, 65)
	mark.StrokeColor = "#e03131"
	mark.StrokeWidth = 3
	mark.FillColor = "transparent"
	mark.Roughness = 0
	w.image.Board.Scene.Elements = append(w.image.Board.Scene.Elements, mark)
	tt := ui.NewTester(win.view, 1100, 780)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	tt.Frame()
	w.image.Board.ChangeProperty("stroke", "#e03131", a.boardOptionsWith(nil, w.doc))
	savePNG(t, filepath.Join(dir, "board-image-annotation.png"), tt)
}

func TestBoardImageClosePromptsAfterEdit(t *testing.T) {
	a, w, win, data := boardImageFixture(t)
	if err := w.image.Board.InsertImage(data, whiteboard.Point{15, 15}); err != nil {
		t.Fatal(err)
	}
	if win.close() || !w.confirmClose {
		t.Fatal("unsaved annotation closed silently")
	}
	a.discardBoardImage(w)
	if !win.close() || !w.closed {
		t.Fatal("discard did not close annotation")
	}
}
