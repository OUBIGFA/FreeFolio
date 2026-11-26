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

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// BenchmarkBoardResize isolates FreeFolio's gesture work from the app shell,
// native rendering and disk I/O. Unselected elements must not be copied for
// every pointer move.
func BenchmarkBoardResize(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s := whiteboard.NewScene()
			for i := range count {
				s.Elements = append(s.Elements, whiteboard.NewElement(whiteboard.KindRectangle, float64(i*150), 100, 100, 80))
			}
			board := NewBoard(s)
			board.selectIDs([]string{s.Elements[0].ID})
			board.before = s.Clone()
			board.mode, board.handle = "resize", 4
			board.selectionBox = whiteboard.SelectionBounds(s, board.SelectedIDs())
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				board.pointerMove(ui.InputEvent{X: float32(110 + i%20), Y: 195}, BoardOptions{})
			}
		})
	}
}

func TestBoardResizeKeepsUnchangedResources(t *testing.T) {
	b := NewBoard(nil)
	selected := whiteboard.NewElement(whiteboard.KindRectangle, 100, 100, 100, 80)
	other := whiteboard.NewElement(whiteboard.KindRectangle, 400, 100, 100, 80)
	b.Scene.Elements = append(b.Scene.Elements, selected, other)
	b.Scene.Files["image"] = &whiteboard.FileData{ID: "image", DataURL: "data:image/png;base64,AAAA"}
	resource := b.Scene.Files["image"]
	b.selectIDs([]string{selected.ID})
	b.pointerDown(ui.InputEvent{X: 200, Y: 180}, BoardOptions{})
	for _, x := range []float32{220, 240, 230} {
		b.pointerMove(ui.InputEvent{X: x, Y: 200}, BoardOptions{})
	}
	if b.Scene.ElementByID(other.ID) != other || b.Scene.Files["image"] != resource {
		t.Fatal("resizing one shape replaced untouched shapes or image resources")
	}
	b.pointerUp(ui.InputEvent{X: 230, Y: 200}, BoardOptions{})
	if e := b.Scene.ElementByID(selected.ID); e.W != 130 || e.H != 100 {
		t.Fatalf("resize accumulated transforms: %v x %v", e.W, e.H)
	}
	if !b.Undo() || b.Scene.ElementByID(selected.ID).W != 100 {
		t.Fatal("gesture was not one undo step")
	}
}

func BenchmarkBoardAILinks(b *testing.B) {
	s := whiteboard.NewScene()
	for i := range 200 {
		e := whiteboard.NewAINode(whiteboard.AIText, float64(i*400), 0)
		n := whiteboard.AINodeOf(e)
		if i > 0 {
			n.Links = []whiteboard.AILink{{Slot: "prompt", From: s.Elements[i-1].ID}}
			_ = whiteboard.SetAINode(e, n)
		}
		s.Elements = append(s.Elements, e)
	}
	board := NewBoard(s)
	board.aiLinks()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		board.aiLinks()
	}
}

// Hover must not rebuild distant freehand outlines on every pointer move.
func BenchmarkBoardHover(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			board := NewBoard(nil)
			for i := range count {
				e := whiteboard.NewElement(whiteboard.KindFreedraw, float64(i*200), 0, 100, 80)
				for j := range 40 {
					e.Points = append(e.Points, whiteboard.Point{float64(j * 2), float64(j % 7)})
				}
				board.Scene.Elements = append(board.Scene.Elements, e)
			}
			board.canvasHit(whiteboard.Point{-100, -100})
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				board.canvasHit(whiteboard.Point{-100, float64(-100 - i%10)})
			}
		})
	}
}

func TestBoardHoverReusesDistantStrokeBounds(t *testing.T) {
	b := NewBoard(nil)
	e := whiteboard.NewElement(whiteboard.KindFreedraw, 100, 100, 100, 80)
	e.Points = []whiteboard.Point{{0, 0}, {40, 20}, {80, 10}}
	b.Scene.Elements = append(b.Scene.Elements, e)
	if allocations := testing.AllocsPerRun(5, func() { b.canvasHit(whiteboard.Point{-100, -100}) }); allocations != 0 {
		t.Fatalf("hovering away from unchanged strokes allocates %v times", allocations)
	}
	b.mode = "erase"
	if allocations := testing.AllocsPerRun(5, func() { b.pointerMove(ui.InputEvent{X: -100, Y: -100}, BoardOptions{}) }); allocations != 0 {
		t.Fatalf("moving the eraser away from unchanged strokes allocates %v times", allocations)
	}
	// Moving a stroke must invalidate its cached bounds for hit testing.
	b.Scene.Translate([]string{e.ID}, whiteboard.Point{400, 0})
	for _, p := range []whiteboard.Point{{100, 100}, {500, 100}, {540, 120}, {580, 110}} {
		if got, want := b.canvasHit(p), b.Scene.Hit(p, 6/b.Zoom, true); got != want {
			t.Fatalf("hit at %v = %v, want %v", p, got, want)
		}
	}
}

func TestBoardAINodeMoveKeepsParsedContent(t *testing.T) {
	b := NewBoard(nil)
	e := whiteboard.NewAINode(whiteboard.AIText, 0, 0)
	b.Scene.Elements = append(b.Scene.Elements, e)
	n := b.aiNode(e)
	b.Scene.Translate([]string{e.ID}, whiteboard.Point{20, 30})
	if b.aiNode(e) != n {
		t.Fatal("moving a card reparsed unchanged node content")
	}
	whiteboard.Touch(e)
	n.Instruction = "changed"
	_ = whiteboard.SetAINode(e, n)
	if b.aiNode(e).Instruction != "changed" {
		t.Fatal("node content cache did not invalidate")
	}
}

// This opt-in measurement uses the pinned Quiet.md scene generator. Tester
// frame durations measure CPU event/build work, not GPU/display latency.
func TestBoardPerformanceReport(t *testing.T) {
	dir := os.Getenv("FREEFOLIO_BOARD_PERF")
	if dir == "" {
		t.Skip("set FREEFOLIO_BOARD_PERF to generated scenes")
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
	if err := os.WriteFile(filepath.Join(dir, "freefolio-board-performance.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
}
