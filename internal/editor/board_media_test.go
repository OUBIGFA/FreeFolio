package editor

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func boardMediaPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 16, 8))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestBoardImagesFollowViewportAndKeepExportComplete(t *testing.T) {
	b := NewBoard(nil)
	t.Cleanup(b.Close)
	for i := range 3 {
		id := fmt.Sprint(i)
		e := whiteboard.NewElement(whiteboard.KindImage, float64(i*3000), 0, 100, 80)
		e.FileID = id
		b.Scene.Elements = append(b.Scene.Elements, e)
		b.Scene.Files[id] = &whiteboard.FileData{ID: id}
	}
	// Orphan resources are retained in files for compatibility, not decoded.
	b.Scene.Files["unused"] = &whiteboard.FileData{ID: "unused"}
	data := boardMediaPNG(t)
	reads := map[string]int{}
	o := BoardOptions{ResolveFile: func(f *whiteboard.FileData) ([]byte, error) {
		reads[f.ID]++
		return data, nil
	}}
	b.lastRect = ui.Rect{W: 800, H: 600}
	b.prepareImages(o)
	if len(b.images) != 1 || b.images["0"] == nil || reads["unused"] != 0 {
		t.Fatalf("decoded images outside the viewport: reads=%v, retained=%d", reads, len(b.images))
	}
	b.PanX = -3000
	b.prepareImages(o)
	if len(b.images) != 1 || b.images["1"] == nil {
		t.Fatalf("pan did not replace distant images: retained=%d", len(b.images))
	}
	b.Scene.Delete([]string{b.Scene.Elements[1].ID})
	b.prepareImages(o)
	if len(b.images) != 0 {
		t.Fatal("deleted image remains decoded")
	}
	// Export has no interactive viewport and must load every live image.
	export := NewBoard(b.Scene.Clone())
	defer export.Close()
	export.prepareImages(o)
	if len(export.images) != 2 || export.images["0"] == nil || export.images["2"] == nil {
		t.Fatal("export did not load every live image")
	}
	if len(b.Scene.Files) != 4 {
		t.Fatal("viewport loading changed persisted resources")
	}
}

func TestBoardMediaLoadsAreBoundedAndDiscardClosedResults(t *testing.T) {
	b := NewBoard(nil)
	data := boardMediaPNG(t)
	done := make(chan func(), 20)
	o := BoardOptions{Dispatch: func(fn func()) { done <- fn }, ResolveFile: func(*whiteboard.FileData) ([]byte, error) { return data, nil }}
	for i := range 8 {
		id := fmt.Sprint(i)
		e := whiteboard.NewElement(whiteboard.KindImage, 0, 0, 100, 80)
		e.FileID = id
		b.Scene.Elements = append(b.Scene.Elements, e)
		b.Scene.Files[id] = &whiteboard.FileData{ID: id}
	}
	b.prepareImages(o)
	if len(b.imagePending) > 2 {
		t.Errorf("scheduled %d decodes at once", len(b.imagePending))
	}
	count := len(b.imagePending)
	b.Close()
	for range count {
		select {
		case apply := <-done:
			apply()
		case <-time.After(5 * time.Second):
			t.Fatal("image decode did not finish")
		}
	}
	if len(b.images) != 0 {
		t.Fatal("closed board retained decoded images")
	}
}

func TestBoardAIMediaFollowsViewport(t *testing.T) {
	b := NewBoard(nil)
	defer b.Close()
	for i := range 2 {
		e := whiteboard.NewAINode(whiteboard.AIImage, float64(i*3000), 0)
		n := whiteboard.AINodeOf(e)
		n.AddOutputs([]whiteboard.AIOutput{{Kind: "image", Path: fmt.Sprintf("%d.png", i)}})
		if err := whiteboard.SetAINode(e, n); err != nil {
			t.Fatal(err)
		}
		b.Scene.Elements = append(b.Scene.Elements, e)
	}
	data := boardMediaPNG(t)
	o := BoardOptions{AI: &BoardAI{ReadFile: func(string) ([]byte, error) { return data, nil }}}
	b.lastRect = ui.Rect{W: 800, H: 600}
	b.aiPrepareMedia(o)
	if len(b.ai.media) != 1 || b.ai.media["0.png"] == nil {
		t.Fatal("offscreen AI result was decoded")
	}
	b.PanX = -3000
	b.aiPrepareMedia(o)
	if len(b.ai.media) != 1 || b.ai.media["1.png"] == nil {
		t.Fatal("AI result cache did not follow the viewport")
	}
}

func TestBoardMediaQueueProgressesAcrossPicturesAndAI(t *testing.T) {
	b := NewBoard(nil)
	defer b.Close()
	data := boardMediaPNG(t)
	done := make(chan func(), 8)
	o := BoardOptions{
		Dispatch:    func(fn func()) { done <- fn },
		ResolveFile: func(*whiteboard.FileData) ([]byte, error) { return data, nil },
		AI:          &BoardAI{ReadFile: func(string) ([]byte, error) { return data, nil }},
	}
	for i := range 3 {
		id := fmt.Sprint(i)
		e := whiteboard.NewElement(whiteboard.KindImage, 0, 0, 100, 80)
		e.FileID = id
		b.Scene.Files[id] = &whiteboard.FileData{ID: id}
		b.Scene.Elements = append(b.Scene.Elements, e)
		node := whiteboard.NewAINode(whiteboard.AIImage, 0, 100)
		n := whiteboard.AINodeOf(node)
		n.AddOutputs([]whiteboard.AIOutput{{Kind: "image", Path: id + ".png"}})
		if err := whiteboard.SetAINode(node, n); err != nil {
			t.Fatal(err)
		}
		b.Scene.Elements = append(b.Scene.Elements, node)
	}
	for range 6 {
		b.prepareImages(o)
		b.aiPrepareMedia(o)
		if pending := len(b.imagePending) + len(b.ai.mediaPending); pending > 2 {
			t.Fatalf("pictures and AI scheduled %d concurrent decodes", pending)
		}
		select {
		case apply := <-done:
			apply()
		case <-time.After(5 * time.Second):
			t.Fatal("queued picture never loaded")
		}
	}
	if len(b.images) != 3 || len(b.ai.media) != 3 {
		t.Fatalf("queue did not drain: pictures=%d AI=%d", len(b.images), len(b.ai.media))
	}
}

func TestBoardMediaDiscardsResultsAfterPan(t *testing.T) {
	b := NewBoard(nil)
	defer b.Close()
	e := whiteboard.NewElement(whiteboard.KindImage, 0, 0, 100, 80)
	e.FileID = "image"
	b.Scene.Elements = append(b.Scene.Elements, e)
	b.Scene.Files[e.FileID] = &whiteboard.FileData{ID: e.FileID}
	b.lastRect = ui.Rect{W: 800, H: 600}
	data := boardMediaPNG(t)
	done := make(chan func(), 1)
	o := BoardOptions{Dispatch: func(fn func()) { done <- fn }, ResolveFile: func(*whiteboard.FileData) ([]byte, error) { return data, nil }}
	b.prepareImages(o)
	b.PanX = -3000
	b.prepareImages(o)
	select {
	case apply := <-done:
		apply()
	case <-time.After(5 * time.Second):
		t.Fatal("image did not load")
	}
	if len(b.images) != 0 || len(b.imagePending) != 0 {
		t.Fatal("distant decode was retained after panning")
	}
	b.PanX = 0
	b.prepareImages(o)
	select {
	case apply := <-done:
		apply()
	case <-time.After(5 * time.Second):
		t.Fatal("returning to the image did not reload it")
	}
	if b.images[e.FileID] == nil {
		t.Fatal("returned image is missing")
	}
}

// Each fixture resource is a full 512 x 512 image. Only the first lies in the
// 800 x 600 viewport; allocation volume is not process working-set memory.
func BenchmarkBoardImageViewport(b *testing.B) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 512, 512))); err != nil {
		b.Fatal(err)
	}
	s := whiteboard.NewScene()
	for i := range 32 {
		id := fmt.Sprint(i)
		e := whiteboard.NewElement(whiteboard.KindImage, float64(i*3000), 0, 512, 512)
		e.FileID = id
		s.Elements = append(s.Elements, e)
		s.Files[id] = &whiteboard.FileData{ID: id}
	}
	o := BoardOptions{ResolveFile: func(*whiteboard.FileData) ([]byte, error) { return data.Bytes(), nil }}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		board := NewBoard(s)
		board.lastRect = ui.Rect{W: 800, H: 600}
		board.prepareImages(o)
		b.ReportMetric(float64(len(board.images)), "images/op")
		board.Close()
	}
}
