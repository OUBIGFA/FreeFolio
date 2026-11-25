package app

import (
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Run with tools/perf's pinned overlay to measure event/view CPU work without
// the test-only software rasterizer. This does not measure native GPU latency.
func BenchmarkWhiteboardFrame(b *testing.B) {
	for _, shell := range []bool{false, true} {
		name := "board"
		if shell {
			name = "app"
		}
		b.Run(name, func(b *testing.B) {
			a, tt := newTestApp(b)
			tt.Frame()
			a.newWhiteboard("")
			tt.Frame()
			s := whiteboard.NewScene()
			for i := range 200 {
				e := whiteboard.NewAINode(whiteboard.AIText, float64(i%20)*420, float64(i/20)*500)
				if i > 0 {
					n := whiteboard.AINodeOf(e)
					n.Links = []whiteboard.AILink{{Slot: "prompt", From: s.Elements[i-1].ID}}
					_ = whiteboard.SetAINode(e, n)
				}
				s.Elements = append(s.Elements, e)
			}
			a.doc.board = editor.NewBoard(s)
			board := a.doc.board
			if !shell {
				tt = ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.whiteboardView(c, a.doc) }) }, 1200, 760)
			}
			tt.Frame()
			board.Zoom = .6
			board.PanX, board.PanY = 0, 0
			for range 4 {
				tt.Frame()
			}
			// Opening a document starts a transition. Let the test clock finish
			// it before timing; Frame alone would settle 20 identical instants.
			tt.Advance(time.Second)
			canvas, ok := tt.Access("手绘白板")
			if !ok || canvas.Bounds.W < 500 || canvas.Bounds.H < 400 {
				b.Fatalf("invalid benchmark viewport: %+v", canvas.Bounds)
			}
			b.Logf("viewport %+v zoom %.2f resident %v", canvas.Bounds, board.Zoom, a.doc.board == board)
			frameStart := a.frame
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				board.PanX = float64(i % 100)
				tt.Frame()
			}
			b.StopTimer()
			if shell {
				b.ReportMetric(float64(a.frame-frameStart)/float64(b.N), "views/op")
			}
			board.Close()
		})
	}
}
