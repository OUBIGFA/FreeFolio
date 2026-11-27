package editor

import (
	"math"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestInsertedImageIsNodeAtViewportCenterAndReversible(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(nil)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	tt.Frame()
	b.Zoom, b.PanX, b.PanY = 1.7, -300, 200
	center := b.ViewCenter()
	if e := b.InsertImage(testPNG(t), whiteboard.Point{9000, 9000}); e != nil {
		t.Fatal(e)
	}
	id := b.SelectedIDs()[0]
	img := b.Scene.ElementByID(id)
	if n := whiteboard.AINodeOf(img); n == nil || n.Role != whiteboard.AIImageInput {
		t.Fatal("image was not inserted as an input node")
	}
	if math.Abs(img.X+img.W/2-center[0]) > 1e-6 || math.Abs(img.Y+img.H/2-center[1]) > 1e-6 || b.Zoom != 1.7 || b.PanX != -300 || b.PanY != 200 {
		t.Fatal("insertion changed viewport or missed center")
	}
	fileID := img.FileID
	img.Crop = &whiteboard.ImageCrop{X: 1, Y: 0, Width: 2, Height: 3, NaturalWidth: 4, NaturalHeight: 4}
	img.Scale = whiteboard.Point{-1, 1}
	b.convertNodeToImage(id, o)
	img = b.Scene.ElementByID(id)
	if whiteboard.IsAINode(img) || img.Kind != whiteboard.KindImage || img.FileID != fileID || len(b.Scene.Files) != 1 || img.Crop == nil || img.Scale[0] != -1 {
		t.Fatal("conversion lost picture data")
	}
	if !b.Undo() || !whiteboard.IsAINode(b.Scene.ElementByID(id)) {
		t.Fatal("conversion cannot undo")
	}
	if !b.Undo() || b.Scene.ElementByID(id) != nil && !b.Scene.ElementByID(id).IsDeleted {
		t.Fatal("insertion cannot undo in one step")
	}
}

func TestImageNodeConversionProtectsLockedAndReadOnly(t *testing.T) {
	b := NewBoard(nil)
	o := (&fakeAI{}).options(t)
	if e := b.InsertImage(testPNG(t), whiteboard.Point{0, 0}); e != nil {
		t.Fatal(e)
	}
	id := b.SelectedIDs()[0]
	b.Scene.ElementByID(id).Locked = true
	b.convertNodeToImage(id, o)
	if !whiteboard.IsAINode(b.Scene.ElementByID(id)) {
		t.Fatal("locked node converted")
	}
	b.Scene.ElementByID(id).Locked = false
	b.ReadOnly = true
	b.convertNodeToImage(id, o)
	if !whiteboard.IsAINode(b.Scene.ElementByID(id)) {
		t.Fatal("read-only node converted")
	}
}

func TestSelectedInputsCreateNodeAtViewportCenter(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(nil)
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	tt.Frame()
	b.Zoom, b.PanX, b.PanY = .7, -400, 80
	source := whiteboard.NewElement(whiteboard.KindNote, 9000, 9000, 200, 200)
	b.Scene.Elements = append(b.Scene.Elements, source)
	b.selectIDs([]string{source.ID})
	center := b.ViewCenter()
	id := b.AIFromSelection(whiteboard.AIText, o)
	e := b.Scene.ElementByID(id)
	if e == nil || math.Abs(e.X+e.W/2-center[0]) > 1e-6 || math.Abs(e.Y+e.H/2-center[1]) > 1e-6 {
		t.Fatal("selection-created node is not centered", e)
	}
}

func TestGeneratedImageConversionKeepsIDAndDownstreamLink(t *testing.T) {
	b := NewBoard(nil)
	f := &fakeAI{}
	o := f.options(t)
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{500, 300}, o)
	f.files["assets/generated.png"] = testPNG(t)
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.Outputs = []whiteboard.AIOutput{{Kind: "image", Path: "assets/generated.png", MIME: "image/png"}}
	})
	target := b.AddAINode(whiteboard.AIImage, whiteboard.Point{1000, 300}, o)
	b.setAILink(target, "image.0", id)
	b.convertNodeToImage(id, o)
	image := b.Scene.ElementByID(id)
	if whiteboard.IsAINode(image) || image.Kind != whiteboard.KindImage || b.Scene.Files[image.FileID].Path != "assets/generated.png" {
		t.Fatal("generated image not converted in place")
	}
	if whiteboard.AINodeOf(b.Scene.ElementByID(target)).LinkAt("image.0") != id {
		t.Fatal("downstream link lost")
	}
	if !b.Undo() || !whiteboard.IsAINode(b.Scene.ElementByID(id)) {
		t.Fatal("generated conversion cannot undo")
	}
}
