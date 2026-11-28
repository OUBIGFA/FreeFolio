package editor

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardAIGeneratedGalleryGrowsWithoutMovingViewportAndUndoRestoresSize(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
	tt.Frame()
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{400, 330}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = "four images" })
	tt.Frame()
	e := b.Scene.ElementByID(id)
	x, y, width, height, zoom, panX, panY := e.X, e.Y, e.W, e.H, b.Zoom, b.PanX, b.PanY
	prepare := o.AI.Prepare
	o.AI.Prepare = func() (*BoardAIRunner, error) {
		runner, err := prepare()
		media := runner.Media
		runner.Media = func(ctx context.Context, job BoardAIJob, report func(string)) ([]BoardAIMedia, error) {
			out, err := media(ctx, job, report)
			if err != nil {
				return nil, err
			}
			return []BoardAIMedia{out[0], out[0], out[0], out[0]}, nil
		}
		return runner, err
	}
	b.RunAI([]string{id}, false, true, o)
	if e.W < 720 || e.H < 1000 || len(b.aiNode(e).Outputs) != 4 {
		t.Fatal("completed batch did not expand its node")
	}
	if e.X != x || e.Y != y || b.Zoom != zoom || b.PanX != panX || b.PanY != panY {
		t.Fatal("generation moved node or viewport")
	}
	encoded, err := b.Scene.Encode()
	if err != nil {
		t.Fatal(err)
	}
	scene, err := whiteboard.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reopened := NewBoard(scene)
	loaded := scene.ElementByID(id)
	if len(reopened.aiPreviewPaths(loaded, reopened.aiNode(loaded))) != 4 || loaded.W != e.W || loaded.H != e.H {
		t.Fatal("reopening lost the gallery or its size")
	}
	if !b.Undo() {
		t.Fatal("generation cannot be undone")
	}
	e = b.Scene.ElementByID(id)
	if e.W != width || e.H != height || len(b.aiNode(e).Outputs) != 0 {
		t.Fatal("undo did not restore both results and geometry")
	}
}

func galleryPNG(t *testing.T, red uint8) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 32, 24))
	draw.Draw(picture, picture.Bounds(), &image.Uniform{C: color.RGBA{R: red, G: 100, B: 150, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestBoardAIImageBatchShowsEveryResult(t *testing.T) {
	for _, count := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := &fakeAI{}
			o := f.options(t)
			o.AI.OpenHistoryImage = func(string, int) {}
			b := NewBoard(whiteboard.NewScene())
			id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{400, 330}, o)
			var outputs []whiteboard.AIOutput
			for i := range count {
				rel := fmt.Sprintf("assets/batch-%d.png", i)
				f.files[rel] = galleryPNG(t, uint8(40+i*40))
				outputs = append(outputs, whiteboard.AIOutput{Kind: "image", Path: rel})
			}
			b.updateAINode(o, id, func(n *whiteboard.AINode) { n.AddOutputs(outputs) })
			tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
			tt.Frame()
			e := b.Scene.ElementByID(id)
			layout := b.aiLayout(e, b.aiNode(e))
			cell := aiHistoryCell(layout.preview, count, 0)
			if cell.W < 300 || cell.H-26 < 300 || layout.preview.H < float32(e.H)*.55 {
				t.Fatalf("multi-image node must give each image ample room: node=%gx%g preview=%+v cell=%+v", e.W, e.H, layout.preview, cell)
			}
			paths := b.aiPreviewPaths(e, b.aiNode(e))
			if len(paths) != count {
				t.Fatalf("batch shows %d of %d images: %v", len(paths), count, paths)
			}
			for i, rel := range paths {
				if rel != outputs[i].Path || b.ai.media[rel] == nil {
					t.Fatalf("batch image %d not loaded: %q", i, rel)
				}
				cell := aiHistoryCell(b.aiLayout(e, b.aiNode(e)).preview, count, i)
				x, y, _, _ := b.canvasRect(e.X+float64(cell.X+cell.W/2), e.Y+float64(cell.Y+(cell.H-26)/2), 0, 0)
				pixel := color.RGBAModel.Convert(tt.Image().At(int(x), int(y))).(color.RGBA)
				if pixel.R != uint8(40+i*40) || pixel.G != 100 || pixel.B != 150 {
					t.Fatalf("tile %d painted wrong image: %+v", i, pixel)
				}
			}
			if dir := os.Getenv("FREEFOLIO_SNAPSHOT"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				file, err := os.Create(filepath.Join(dir, fmt.Sprintf("ai-gallery-%d.png", count)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(file, tt.Image())
				_ = file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestBoardAIGalleryOpensAndCombinesHistoryWithoutChangingOutput(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{400, 330}, o)
	for run := range 2 {
		var outputs []whiteboard.AIOutput
		for i := range 4 {
			rel := fmt.Sprintf("assets/history-%d.png", run*4+i)
			f.files[rel] = galleryPNG(t, uint8(30+run*80+i*20))
			outputs = append(outputs, whiteboard.AIOutput{Kind: "image", Path: rel})
		}
		b.updateAINode(o, id, func(n *whiteboard.AINode) { n.AddOutputs(outputs) })
	}
	e := b.Scene.ElementByID(id)
	opened := -1
	o.AI.OpenHistoryImage = func(nodeID string, index int) {
		if nodeID != id {
			t.Fatal("wrong node opened")
		}
		opened = index
	}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
	tt.Frame()
	for index := 4; index < 8; index++ {
		if err := tt.Click(fmt.Sprintf("查看第 %d 张图片", index+1)); err != nil {
			t.Fatal(err)
		}
		if opened != index || b.aiNode(e).Current != 4 {
			t.Fatal("view changed output or opened wrong image")
		}
	}
	if err := tt.Click("对比图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for i, index := range []int{0, 5, 2, 7} {
		if err := tt.Click("对比图片 " + string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(fmt.Sprintf("第 %d 张 · 第 %d 次", index+1, index/4+1)); err != nil {
			t.Fatal(err)
		}
	}
	tt.Frame()
	want := []string{"assets/history-0.png", "assets/history-5.png", "assets/history-2.png", "assets/history-7.png"}
	if got := b.aiPreviewPaths(e, b.aiNode(e)); !slices.Equal(got, want) || b.aiNode(e).Current != 4 {
		t.Fatalf("custom four-way comparison = %v", got)
	}
	for _, mode := range []string{"两格", "滑杆", "四格"} {
		if err := tt.Click(mode); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if _, ok := tt.Access("图片对比分隔线"); ok != (mode == "滑杆") {
			t.Fatalf("wrong interaction for %s", mode)
		}
	}
	if err := tt.Click("退出对比"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("使用第 7 张图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if b.aiNode(e).Current != 6 || len(b.aiPreviewPaths(e, b.aiNode(e))) != 4 {
		t.Fatal("selecting output hid the rest of its batch")
	}
	for _, readOnly := range []bool{true, false} {
		b.ReadOnly, o.ReadOnly, e.Locked = readOnly, readOnly, !readOnly
		before := string(e.CustomData)
		b.aiSelectImage(o, e, b.aiNode(e), 0)
		tt.Frame()
		if err := tt.Click("查看第 2 张图片"); err != nil {
			t.Fatal(err)
		}
		if opened != 1 || string(e.CustomData) != before {
			t.Fatal("read-only viewing changed saved output")
		}
	}
}
