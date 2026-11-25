package editor

import (
	"bytes"
	"context"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

func TestImageViewerFitsBeforeFirstPaint(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1600, 1000))); err != nil {
		t.Fatal(err)
	}
	a, err := NewImageAnnotation(data.Bytes(), nil, whiteboard.Point{1, 1})
	if err != nil {
		t.Fatal(err)
	}
	var painted []float64
	tt := ui.NewTester(func(c *ui.Context) {
		ImageAnnotationView(c, a, true, nil)
		ui.Box(c.Key("paint-observer")).Absolute().Size(1, 1).Draw(func(_ *ui.Painter, _ ui.Rect) {
			painted = append(painted, a.Board.Zoom)
		})
	}, 800, 600)
	tt.Frame()
	if len(painted) == 0 {
		t.Fatal("viewer was not painted")
	}
	for _, zoom := range painted {
		if math.Abs(zoom-.425) > .001 {
			t.Fatalf("viewer flashed at %v before fitting to 0.425; frames %v", zoom, painted)
		}
	}
}

func TestImageAnnotationPreservesSizeAndRendersMarks(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.White)
		}
	}
	var buf bytes.Buffer
	if e := png.Encode(&buf, img); e != nil {
		t.Fatal(e)
	}
	a, e := NewImageAnnotation(buf.Bytes(), nil, whiteboard.Point{1, 1})
	if e != nil {
		t.Fatal(e)
	}
	mark := whiteboard.NewElement(whiteboard.KindRectangle, 20, 20, 35, 25)
	mark.FillColor = "#ff0000"
	mark.FillStyle = whiteboard.FillSolid
	mark.StrokeColor = "#ff0000"
	mark.Roughness = 0
	a.Board.Scene.Elements = append(a.Board.Scene.Elements, mark)
	data, e := a.PNG()
	if e != nil {
		t.Fatal(e)
	}
	out, e := png.Decode(bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	if out.Bounds().Dx() != 120 || out.Bounds().Dy() != 80 {
		t.Fatalf("wrong export size: %v", out.Bounds())
	}
	r, g, b, _ := out.At(35, 30).RGBA()
	if r < 50000 || g > 10000 || b > 10000 {
		t.Fatalf("mark not rendered: %d %d %d", r, g, b)
	}
	r, g, b, _ = out.At(3, 3).RGBA()
	if r < 60000 || g < 60000 || b < 60000 {
		t.Fatal("base image lost")
	}
}

func TestAnnotatedGeneratorReceivesSavedPictureBytes(t *testing.T) {
	b := NewBoard(nil)
	data := testPNG(t)
	called := false
	o := BoardOptions{ResolveFile: func(f *whiteboard.FileData) ([]byte, error) { return data, nil }, AI: &BoardAI{Prepare: func() (*BoardAIRunner, error) {
		return &BoardAIRunner{Media: func(ctx context.Context, job BoardAIJob, progress func(string)) ([]BoardAIMedia, error) {
			called = true
			if len(job.Images) != 1 || !bytes.Equal(job.Images[0].Data, data) {
				t.Fatal("annotated image not passed to provider")
			}
			return []BoardAIMedia{{Output: whiteboard.AIOutput{Kind: "image", Path: "assets/result.png"}, Data: data}}, nil
		}}, nil
	}}}
	source := b.AddAINode(whiteboard.AIImage, whiteboard.Point{}, o)
	o.AI.Default = func(whiteboard.AIRole) BoardAIModel { return BoardAIModel{Connection: "test", Model: "image-test"} }
	_, id, e := b.AddAnnotatedReference(source, "assets/annotated.png", 4, 4, true, o)
	if e != nil {
		t.Fatal(e)
	}
	if called {
		t.Fatal("creating a generation node sent a network request")
	}
	b.RunAI([]string{id}, false, false, o)
	if !called || b.LastError() != nil {
		t.Fatalf("run: %v", b.LastError())
	}
}

func TestAnnotationRefusesIMEPreviewAndProtectsBase(t *testing.T) {
	a, e := NewImageAnnotation(testPNG(t), nil, whiteboard.Point{1, 1})
	if e != nil {
		t.Fatal(e)
	}
	b := a.Board
	o := BoardOptions{ImageAnnotation: true}
	b.RunAction("select-all", o)
	b.RunAction("delete", o)
	if base := b.Scene.ElementByID(b.annotationBase); base == nil || base.IsDeleted {
		t.Fatal("select-all deleted the base image")
	}
	b.editing = &boardTextSession{board: b, hasMarked: true, shown: "candidate"}
	if _, e = a.Snapshot(); e == nil {
		t.Fatal("IME candidate baked into annotation")
	}
	b.editing = nil
}

func TestAnnotationInputPreservesCropFlipAndReadonlyNavigation(t *testing.T) {
	data := testPNG(t)
	a, e := NewImageAnnotation(data, &whiteboard.ImageCrop{X: 1, Y: 0, Width: 2, Height: 4, NaturalWidth: 4, NaturalHeight: 4}, whiteboard.Point{-1, 1})
	if e != nil {
		t.Fatal(e)
	}
	if a.Width != 2 || a.Height != 4 {
		t.Fatalf("crop: %dx%d", a.Width, a.Height)
	}
	tt := ui.NewTester(func(c *ui.Context) { ImageAnnotationView(c, a, true, nil) }, 800, 600)
	tt.Frame()
	tt.Frame()
	before := a.Board.Zoom
	a.Board.input(ui.InputEvent{Kind: ui.InputScroll, X: 300, Y: 200, DY: -40}, BoardOptions{ImageAnnotation: true, ReadOnly: true})
	if a.Board.Zoom <= before {
		t.Fatal("wheel did not zoom")
	}
	if len(a.Board.Scene.Elements) != 1 {
		t.Fatal("viewing changed image content")
	}
}

func TestAnnotatedReferenceAddsConnectedNodesInOneUndo(t *testing.T) {
	b := NewBoard(nil)
	o := BoardOptions{AI: &BoardAI{}}
	source := b.AddAINode(whiteboard.AIImage, whiteboard.Point{}, o)
	original := b.Scene.Clone()
	input, generator, err := b.AddAnnotatedReference(source, "assets/marked.png", 120, 80, true, o)
	if err != nil {
		t.Fatal(err)
	}
	if input == "" || generator == "" || b.Scene.ElementByID(source) == nil {
		t.Fatal("reference nodes missing")
	}
	inputs := b.Scene.AIInputs(generator)
	if len(inputs) != 1 || inputs[0].From != input {
		t.Fatalf("unconnected generation: %+v", inputs)
	}
	file := b.Scene.Files[b.Scene.ElementByID(input).FileID]
	if file == nil || file.Path != "assets/marked.png" || file.DataURL != "" {
		t.Fatal("annotation must reference an attachment")
	}
	if !b.Undo() || len(b.Scene.Elements) != len(original.Elements) {
		t.Fatal("annotation pair is not one undo")
	}
	b.ReadOnly = true
	if _, _, err = b.AddAnnotatedReference(source, "assets/blocked.png", 10, 10, true, o); err == nil {
		t.Fatal("read-only board edited")
	}
}
