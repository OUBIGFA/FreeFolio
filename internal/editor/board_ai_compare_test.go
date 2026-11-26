package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func testColoredPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBoardAICompareModesSwapAndReadOnly(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	if err := b.InsertImage(testColoredPNG(t, 8, 8, color.NRGBA{R: 220, G: 40, B: 40, A: 255}), whiteboard.Point{120, 200}); err != nil {
		t.Fatal(err)
	}
	imgA := b.Scene.ElementByID(b.SelectedIDs()[0])
	if err := b.InsertImage(testColoredPNG(t, 8, 8, color.NRGBA{R: 40, G: 80, B: 220, A: 255}), whiteboard.Point{120, 420}); err != nil {
		t.Fatal(err)
	}
	imgB := b.Scene.ElementByID(b.SelectedIDs()[0])

	cmpID := b.AddAINode(whiteboard.AICompare, whiteboard.Point{600, 320}, o)
	cmpEl := b.Scene.ElementByID(cmpID)
	b.setAILink(cmpID, "a", imgA.ID)
	b.setAILink(cmpID, "b", imgB.ID)
	b.selectIDs([]string{cmpID})
	b.fitted = true

	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	tt.Frame()
	if status, failed := b.aiStatus(cmpEl, b.aiNode(cmpEl)); failed || status != "对比中" {
		t.Fatalf("compare node status = %q (failed=%v), want 对比中", status, failed)
	}
	if _, ok := tt.Access("对比模式"); !ok {
		t.Fatal("missing compare mode selector")
	}
	if _, ok := tt.Access("对调"); !ok {
		t.Fatal("missing swap button")
	}

	for _, mode := range compareModeValues {
		cmpEl = b.Scene.ElementByID(cmpID)
		b.setCompareMode(o, cmpEl, mode)
		tt.Frame()
		cmpEl = b.Scene.ElementByID(cmpID)
		if got := b.aiCompareMode(cmpEl, b.aiNode(cmpEl)); got != mode {
			t.Fatalf("compare mode = %q, want %q", got, mode)
		}
	}
	for _, bg := range compareBgValues {
		cmpEl = b.Scene.ElementByID(cmpID)
		b.setCompareBg(o, cmpEl, bg)
		tt.Frame()
		cmpEl = b.Scene.ElementByID(cmpID)
		if got := b.aiCompareBg(cmpEl, b.aiNode(cmpEl)); got != bg {
			t.Fatalf("compare bg = %q, want %q", got, bg)
		}
	}

	cmpEl = b.Scene.ElementByID(cmpID)
	b.swapCompareInputs(o, cmpEl)
	tt.Frame()
	cmpEl = b.Scene.ElementByID(cmpID)
	n := b.aiNode(cmpEl)
	if n.LinkAt("a") != imgB.ID || n.LinkAt("b") != imgA.ID {
		t.Fatalf("swap failed: a=%s b=%s", n.LinkAt("a"), n.LinkAt("b"))
	}
	if !b.Undo() {
		t.Fatal("swap could not be undone")
	}
	cmpEl = b.Scene.ElementByID(cmpID)
	n = b.aiNode(cmpEl)
	if n.LinkAt("a") != imgA.ID || n.LinkAt("b") != imgB.ID {
		t.Fatalf("undo swap failed: a=%s b=%s", n.LinkAt("a"), n.LinkAt("b"))
	}

	// Read-only browsing allows temporary mode/background inspection without mutating the node.
	o.ReadOnly = true
	tt.Frame()
	cmpEl = b.Scene.ElementByID(cmpID)
	b.setCompareMode(o, cmpEl, "side")
	b.setCompareBg(o, cmpEl, "dark")
	tt.Frame()
	cmpEl = b.Scene.ElementByID(cmpID)
	if b.aiCompareMode(cmpEl, b.aiNode(cmpEl)) != "side" || b.aiCompareBg(cmpEl, b.aiNode(cmpEl)) != "dark" {
		t.Fatal("read-only compare override failed")
	}
	if b.aiNode(cmpEl).Params.CompareMode == "side" || b.aiNode(cmpEl).Params.CompareBg == "dark" {
		t.Fatal("read-only compare override mutated stored node")
	}
}

func TestComputeCompareDiffImage(t *testing.T) {
	a := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	b := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			a.SetNRGBA(x, y, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
			b.SetNRGBA(x, y, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
		}
	}
	same := computeCompareDiffImage(a, b, nil, nil, false, false, false, false)
	if px := same.NRGBAAt(1, 1); px.R != 0 || px.G != 0 || px.B != 0 || px.A != 255 {
		t.Fatalf("identical images produced non-zero diff: %+v", px)
	}
	b.SetNRGBA(2, 2, color.NRGBA{R: 100, G: 100, B: 100, A: 55})
	diff := computeCompareDiffImage(a, b, nil, nil, false, false, false, false)
	if px := diff.NRGBAAt(2, 2); px.R == 0 || px.B == 0 {
		t.Fatalf("alpha difference was not highlighted: %+v", px)
	}
}

func TestBoardAICompareKeepsOffscreenUpstreamImageLoadedAtHighZoom(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	pngA := testColoredPNG(t, 8, 8, color.NRGBA{R: 220, G: 40, B: 40, A: 255})
	pngB := testColoredPNG(t, 8, 8, color.NRGBA{R: 40, G: 180, B: 220, A: 255})
	o.AI.ReadFile = func(rel string) ([]byte, error) {
		switch rel {
		case "assets/a.png":
			return pngA, nil
		case "assets/b.png":
			return pngB, nil
		}
		return nil, os.ErrNotExist
	}
	b := NewBoard(whiteboard.NewScene())
	// Place both upstream AIImage nodes far off-screen to the left.
	aID := b.AddAINode(whiteboard.AIImage, whiteboard.Point{-2400, 100}, o)
	bID := b.AddAINode(whiteboard.AIImage, whiteboard.Point{-2400, 700}, o)
	b.updateAINode(o, aID, func(n *whiteboard.AINode) {
		n.Outputs = []whiteboard.AIOutput{{Kind: "image", Path: "assets/a.png", MIME: "image/png"}}
	})
	b.updateAINode(o, bID, func(n *whiteboard.AINode) {
		n.Outputs = []whiteboard.AIOutput{{Kind: "image", Path: "assets/b.png", MIME: "image/png"}}
	})

	cmpID := b.AddAINode(whiteboard.AICompare, whiteboard.Point{400, 300}, o)
	b.setAILink(cmpID, "a", aID)
	b.setAILink(cmpID, "b", bID)

	// Zoom to 80% centered on the compare node so both upstream nodes are outside the viewport.
	b.Zoom, b.PanX, b.PanY = 0.8, 200, 100
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	tt.Frame()
	tt.Frame()
	if b.ai.media["assets/a.png"] == nil || b.ai.media["assets/b.png"] == nil {
		t.Fatalf("compare node evicted or failed to load offscreen upstream bitmaps: a=%v b=%v", b.ai.media["assets/a.png"] != nil, b.ai.media["assets/b.png"] != nil)
	}
}

func TestBoardAICompareHandleUsableAtLowZoom(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	if err := b.InsertImage(testColoredPNG(t, 8, 8, color.NRGBA{R: 220, G: 40, B: 40, A: 255}), whiteboard.Point{120, 200}); err != nil {
		t.Fatal(err)
	}
	imgA := b.Scene.ElementByID(b.SelectedIDs()[0])
	if err := b.InsertImage(testColoredPNG(t, 8, 8, color.NRGBA{R: 40, G: 80, B: 220, A: 255}), whiteboard.Point{120, 420}); err != nil {
		t.Fatal(err)
	}
	imgB := b.Scene.ElementByID(b.SelectedIDs()[0])
	cmpID := b.AddAINode(whiteboard.AICompare, whiteboard.Point{600, 320}, o)
	b.setAILink(cmpID, "a", imgA.ID)
	b.setAILink(cmpID, "b", imgB.ID)

	b.Zoom, b.PanX, b.PanY = 0.25, 300, 200
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 800)
	tt.Frame()
	if _, ok := tt.Access("图片对比分隔线"); !ok {
		t.Fatal("compare divider handle is missing at 25% zoom")
	}
}
