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
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardAIHistorySelectionFeedsDownstreamAndCompareStaysLocal(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
	tt.Frame()
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{400, 330}, o)
	for i := range 10 {
		rel := fmt.Sprintf("assets/history-%d.png", i)
		picture := image.NewRGBA(image.Rect(0, 0, 16, 8))
		draw.Draw(picture, picture.Bounds(), &image.Uniform{C: color.RGBA{R: uint8(20 + i*20), G: 100, B: 150, A: 255}}, image.Point{}, draw.Src)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, picture); err != nil {
			t.Fatal(err)
		}
		f.files[rel] = encoded.Bytes()
		b.updateAINode(o, id, func(n *whiteboard.AINode) {
			n.AddOutputs([]whiteboard.AIOutput{{Kind: "image", Path: rel, MIME: "image/png"}})
		})
	}
	tt.Frame()
	if err := tt.Click("图片历史"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("第 3 张 · 第 3 次"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	e := b.Scene.ElementByID(id)
	if n := b.aiNode(e); n.Current != 2 {
		t.Fatalf("selected = %d", n.Current)
	}
	if err := tt.Click("对比图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("退出") {
		t.Fatal("comparison does not show an exit action")
	}
	if err := tt.Click("对比图片 A"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("第 1 张 · 第 1 次"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("对比图片 B"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("第 8 张 · 第 8 次"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	paths := b.aiPreviewPaths(e, b.aiNode(e))
	if len(paths) != 2 || paths[0] != "assets/history-0.png" || paths[1] != "assets/history-7.png" {
		t.Fatalf("wrong pair: %v", paths)
	}
	if b.ai.media[paths[0]] == nil || b.ai.media[paths[1]] == nil {
		t.Fatal("comparison images not loaded")
	}
	box, ok := tt.Access("图片对比分隔线")
	if !ok {
		t.Fatal("divider missing")
	}
	pixels := tt.Image()
	for _, sample := range []struct {
		x   float32
		red uint8
	}{{.25, 20}, {.75, 160}} {
		pixel := color.RGBAModel.Convert(pixels.At(int(box.Bounds.X+box.Bounds.W*sample.x), int(box.Bounds.Y+box.Bounds.H*.4))).(color.RGBA)
		if pixel.R != sample.red || pixel.G != 100 || pixel.B != 150 {
			t.Fatalf("comparison rendered wrong image: %+v", pixel)
		}
	}
	x, y := e.X, e.Y
	drag(tt, box.Bounds.X+box.Bounds.W/2, box.Bounds.Y+box.Bounds.H/2, box.Bounds.X+box.Bounds.W*.75, box.Bounds.Y+box.Bounds.H/2)
	if e.X != x || e.Y != y {
		t.Fatal("comparison dragged node")
	}
	h := b.aiHistory(e, b.aiNode(e))
	if h.split < .7 || h.split > .8 {
		t.Fatalf("divider = %f", h.split)
	}
	tt.Key(0, ui.KeyHome)
	tt.Frame()
	if h.split != 0 {
		t.Fatalf("keyboard divider = %f", h.split)
	}
	if b.aiNode(e).Current != 2 {
		t.Fatal("comparison changed downstream selection")
	}
	if dir := os.Getenv("FREEFOLIO_SNAPSHOT"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		h.split = .5
		tt.Frame()
		file, err := os.Create(filepath.Join(dir, "ai-image-history.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	next := b.AddAINode(whiteboard.AIImage, whiteboard.Point{900, 330}, o)
	b.updateAINode(o, next, func(n *whiteboard.AINode) { n.Instruction = "edit" })
	b.edit(o, func() { _ = b.connectAI(e, b.Scene.ElementByID(next), true) })
	b.RunAI([]string{next}, false, true, o)
	if len(f.jobs) != 1 || len(f.jobs[0].Images) != 1 || !bytes.Equal(f.jobs[0].Images[0].Data, f.files["assets/history-2.png"]) {
		t.Fatalf("downstream did not receive selected image: %+v", f.jobs)
	}
	// Read-only and locked browsing remains available without mutating the file.
	for _, readOnly := range []bool{true, false} {
		b.ReadOnly, e.Locked = readOnly, !readOnly
		before := string(e.CustomData)
		b.aiSelectImage(o, e, b.aiNode(e), 5)
		if b.aiHistoryCurrent(e, b.aiNode(e)) != 5 || string(e.CustomData) != before {
			t.Fatal("read-only browsing changed stored output")
		}
	}
}

func TestBoardAIFreshImageBatchFeedsOnlyDisplayedResult(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	source := b.AddAINode(whiteboard.AIImage, whiteboard.Point{}, o)
	target := b.AddAINode(whiteboard.AIImage, whiteboard.Point{600, 0}, o)
	b.updateAINode(o, source, func(n *whiteboard.AINode) { n.Instruction = "two images" })
	b.edit(o, func() { _ = b.connectAI(b.Scene.ElementByID(source), b.Scene.ElementByID(target), true) })
	prepare := o.AI.Prepare
	o.AI.Prepare = func() (*BoardAIRunner, error) {
		runner, err := prepare()
		media := runner.Media
		runner.Media = func(ctx context.Context, job BoardAIJob, report func(string)) ([]BoardAIMedia, error) {
			out, err := media(ctx, job, report)
			if job.NodeID == source {
				out = append(out, out[0])
			}
			return out, err
		}
		return runner, err
	}
	b.RunAI([]string{target}, true, false, o)
	if len(f.jobs) != 2 || len(f.jobs[1].Images) != 1 {
		t.Fatalf("downstream received whole batch instead of displayed image: %+v", f.jobs)
	}
}

func TestBoardSaveAsUsesBrowsedImageWithoutChangingOutput(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	var saved string
	o.AI.SaveImage = func(rel string) { saved = rel }
	b := NewBoard(whiteboard.NewScene())
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{360, 330}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) {
		n.AddOutputs([]whiteboard.AIOutput{{Kind: "image", Path: "a.png"}, {Kind: "image", Path: "b.webp"}})
	})
	e := b.Scene.ElementByID(id)
	b.ReadOnly, o.ReadOnly = true, true
	n := b.aiNode(e)
	current := n.Current
	b.aiSelectImage(o, e, n, 1)
	tester := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
	tester.Frame()
	if err := tester.Click("图片另存为"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if saved != "b.webp" || b.aiNode(e).Current != current {
		t.Fatalf("saved %q, selection %d", saved, b.aiNode(e).Current)
	}
}

func TestCutoutNodeRunsWithOnePictureAndNoPrompt(t *testing.T) {
	f := &fakeAI{}
	options := f.options(t)
	board := NewBoard(whiteboard.NewScene())
	source := board.AddAINode(whiteboard.AIImage, whiteboard.Point{300, 250}, options)
	target := board.AddAINode(whiteboard.AICutout, whiteboard.Point{850, 250}, options)
	prompt := board.AddAINode(whiteboard.AIPrompt, whiteboard.Point{0, 0}, options)
	board.updateAINode(options, target, func(n *whiteboard.AINode) {
		n.Connection, n.Instruction = "local", "saved subject note"
		// Old links must not read an empty upstream prompt or reach the runner.
		n.Links = []whiteboard.AILink{{Slot: "prompt", From: prompt}}
	})
	const rel = "assets/source.png"
	f.files[rel] = testPNG(t)
	board.updateAINode(options, source, func(n *whiteboard.AINode) {
		n.AddOutputs([]whiteboard.AIOutput{{Kind: "image", Path: rel, MIME: "image/png"}})
	})
	if err := board.connectAI(board.Scene.ElementByID(source), board.Scene.ElementByID(target), true); err != nil {
		t.Fatal(err)
	}
	board.RunAI([]string{target}, false, true, options)
	result := board.aiNode(board.Scene.ElementByID(target))
	if result.Instruction != "saved subject note" || result.LinkAt("prompt") != prompt {
		t.Fatal("running an image-only model erased its saved prompt")
	}
	if len(f.jobs) != 1 || len(f.jobs[0].Images) != 1 || f.jobs[0].Prompt != "" || !bytes.Equal(f.jobs[0].Images[0].Data, f.files[rel]) || result.Output() == nil || result.Output().Kind != "image" {
		t.Fatalf("cutout run: %+v %+v", result, f.jobs)
	}
}

func TestBoardAIImageRunButtonAppendsHistory(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
	tt.Frame()
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{400, 330}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = "a cat" })
	tt.Frame()
	for range 2 {
		if err := tt.Click("运行此节点"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	n := whiteboard.AINodeOf(b.Scene.ElementByID(id))
	if len(n.Outputs) != 2 || n.Outputs[1].Run != 2 {
		t.Fatalf("second manual run must append a new result: %+v", n)
	}
	if _, ok := tt.Access("图片历史"); !ok {
		t.Fatal("history selector missing from node")
	}
	if err := tt.Click("上张图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := whiteboard.AINodeOf(b.Scene.ElementByID(id)).Current; got != 0 {
		t.Fatalf("history selection = %d", got)
	}
	if err := tt.Click("对比图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"对比图片 A", "对比图片 B", "图片对比分隔线"} {
		if _, ok := tt.Access(label); !ok {
			t.Fatalf("missing %s", label)
		}
	}
}

func TestBoardAIImageHistoryBarCollapsesAndExpandsLayout(t *testing.T) {
	f := &fakeAI{}
	o := f.options(t)
	b := NewBoard(whiteboard.NewScene())
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1200, 900)
	tt.Frame()
	id := b.AddAINode(whiteboard.AIImage, whiteboard.Point{400, 330}, o)
	b.updateAINode(o, id, func(n *whiteboard.AINode) { n.Instruction = "a mountain" })
	tt.Frame()
	e := b.Scene.ElementByID(id)
	if l := b.aiLayout(e, b.aiNode(e)); l.history.H != 0 {
		t.Fatalf("empty node should omit history bar, got %v", l.history.H)
	}
	if err := tt.Click("运行此节点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if l := b.aiLayout(e, b.aiNode(e)); l.history.H != 0 || tt.HasText("对比") {
		t.Fatalf("single output should omit history bar: h=%v", l.history.H)
	}
	if err := tt.Click("运行此节点"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	l1 := b.aiLayout(e, b.aiNode(e))
	if l1.history.H != 26 || l1.history.Y <= l1.preview.Y {
		t.Fatalf("two outputs should show 1-row history bar below preview: preview=%+v history=%+v", l1.preview, l1.history)
	}
	if err := tt.Click("对比图片"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	l2 := b.aiLayout(e, b.aiNode(e))
	if l2.history.H <= l1.history.H || l2.preview.H >= l1.preview.H {
		t.Fatalf("compare mode did not update layout immediately: before=%+v after=%+v", l1, l2)
	}
}
