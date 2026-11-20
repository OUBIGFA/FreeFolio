package editor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// This opt-in measurement uses the pinned Quiet.md scene generator. Tester
// frame durations measure CPU event/build work, not GPU/display latency.
func TestBoardPerformanceReport(t *testing.T) {
	dir := os.Getenv("FREEMD_BOARD_PERF")
	if dir == "" {
		t.Skip("set FREEMD_BOARD_PERF to generated scenes")
	}
	type result struct {
		Shapes, Elements int
		OpenMS           float64
		Scenario         string
		P50MS, P95MS     float64
	}
	var results []result
	for _, n := range []int{200, 1000, 3000, 10000} {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("bench-%d.excalidraw", n)))
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		scene, err := whiteboard.Decode(string(data))
		if err != nil {
			t.Fatal(err)
		}
		b := NewBoard(scene)
		tt := ui.NewTester(func(c *ui.Context) { BoardView(c, b, BoardOptions{}) }, 1280, 800)
		b.RequestFocus = true
		tt.Frame()
		tt.Frame()
		open := float64(time.Since(start).Microseconds()) / 1000
		for _, scenario := range []string{"overview-pan", "100%-pan", "zoom", "drag-one", "drag-all", "undo"} {
			if scenario == "100%-pan" {
				b.Zoom = 1
				b.PanX = 100
				b.PanY = 100
			}
			durations := make([]float64, 20)
			var ids []string
			if scenario == "drag-all" {
				for _, e := range scene.Elements {
					ids = append(ids, e.ID)
				}
			} else {
				ids = []string{scene.Elements[0].ID}
			}
			for i := range durations {
				start = time.Now()
				switch scenario {
				case "overview-pan", "100%-pan":
					b.PanX += 3
				case "zoom":
					b.zoomAt(640, 400, 1.01)
				case "drag-one", "drag-all":
					b.Scene.Translate(ids, whiteboard.Point{2, 0})
				case "undo":
					b.History.Begin(b.Scene)
					b.Scene.Translate(ids, whiteboard.Point{2, 0})
					b.History.Commit(b.Scene)
					b.Undo()
				}
				tt.Frame()
				durations[i] = float64(time.Since(start).Microseconds()) / 1000
			}
			sort.Float64s(durations)
			results = append(results, result{n, len(scene.Elements), open, scenario, durations[10], durations[19]})
		}
		b.Close()
		runtime.GC()
	}
	data, err := json.MarshalIndent(struct {
		Platform      string
		Width, Height int
		Results       []result
	}{runtime.GOOS + "/" + runtime.GOARCH, 1280, 800, results}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "freemd-board-performance.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
}
