package app

import (
	"image/color"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// The Tester's settling can hide an empty first frame. Sample the preceding
// presented image at every view pass, not just the final settled screenshot.
func TestSplitKeepsDocumentVisibleThroughoutOpening(t *testing.T) {
	for _, files := range []bool{false, true} {
		t.Run(map[bool]string{false: "space", true: "files"}[files], func(t *testing.T) {
			a, _ := newTestApp(t)
			scene := whiteboard.NewScene()
			scene.AppState.ViewBackgroundColor = "#d73355"
			raw, err := scene.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if files {
				if !a.openSystemFile(externalFile(t, "Canvas.excalidraw", raw)) {
					t.Fatal("open board")
				}
			} else {
				a.newWhiteboard("")
				a.doc.board.Scene.AppState.ViewBackgroundColor = "#d73355"
			}
			var tt *ui.Tester
			watch := false
			frames, missing := 0, 0
			tt = ui.NewTester(func(c *ui.Context) {
				if watch && tt != nil {
					frames++
					found := false
					img := tt.Image()
					for y := 150; y < 650 && !found; y += 40 {
						for x := 80; x < 800; x += 40 {
							pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
							if pixel.R > 180 && pixel.G < 90 && pixel.B < 130 {
								found = true
								break
							}
						}
					}
					if !found {
						missing++
					}
				}
				a.View(c)
			}, 1200, 760)
			settle(tt)
			watch = true
			if err := tt.Click("添加分屏"); err != nil {
				t.Fatal(err)
			}
			for range 18 {
				tt.Advance(time.Second / 60)
				tt.Frame()
			}
			if frames == 0 || missing != 0 {
				t.Fatalf("opening split presented %d blank document samples out of %d", missing, frames)
			}
		})
	}
}
