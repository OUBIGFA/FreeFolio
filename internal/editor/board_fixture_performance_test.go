package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Opt-in: point FREEFOLIO_BOARD_FIXTURE at an isolated scene copy. Run with
// tools/perf's overlay for CPU scene timings without software rasterization.
func TestBoardFixturePerformance(t *testing.T) {
	file := os.Getenv("FREEFOLIO_BOARD_FIXTURE")
	if file == "" {
		t.Skip("set FREEFOLIO_BOARD_FIXTURE to an isolated scene copy")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	scene, err := whiteboard.Decode(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	b := NewBoard(scene)
	defer b.Close()
	b.PrepareText()
	t.Logf("decode: %v, elements: %d", time.Since(start), len(scene.Elements))
	updates := make(chan func(), 8)
	read := func(rel string) ([]byte, error) {
		return os.ReadFile(filepath.Join(filepath.Dir(file), filepath.FromSlash(rel)))
	}
	o := BoardOptions{Dispatch: func(fn func()) { updates <- fn }, ResolveFile: func(f *whiteboard.FileData) ([]byte, error) { return read(f.Path) }, AI: &BoardAI{ReadFile: read}}
	tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, o) }, 1240, 800)
	start = time.Now()
	tt.Frame()
	t.Logf("first frame: %v, zoom: %.3f, pending: %d", time.Since(start), b.Zoom, b.imageLoads)
	deadline := time.After(30 * time.Second)
	for b.imageLoads > 0 {
		select {
		case fn := <-updates:
			fn()
			tt.Frame()
		case <-deadline:
			t.Fatal("image loading timed out")
		}
	}
	t.Logf("images ready: %v, images: %d, previews: %d, errors: %d/%d", time.Since(start), len(b.images), len(b.ai.media), len(b.imageErrors), len(b.ai.mediaErr))
	for _, zoom := range []float64{b.Zoom, .6, 1} {
		b.Zoom = zoom
		durations := make([]time.Duration, 100)
		for i := range durations {
			start = time.Now()
			tt.Frame()
			durations[i] = time.Since(start)
		}
		slices.Sort(durations)
		t.Logf("zoom %.3f p50 %v p95 %v max %v", zoom, durations[50], durations[95], durations[99])
	}
	for _, err := range b.imageErrors {
		t.Log(fmt.Sprint(err))
	}
}
