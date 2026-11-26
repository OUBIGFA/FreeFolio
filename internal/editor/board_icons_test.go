package editor

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBoardIconsRasterizeAtDisplayedSize(t *testing.T) {
	for _, name := range []string{"text", "image", "group", "sticky-note", "sparkles", "player-play", "player-stop", "trash"} {
		for _, zoom := range []float32{0.5, 1, 1.25, 1.5, 2, 4} {
			t.Run(fmt.Sprintf("%s/%g", name, zoom), func(t *testing.T) {
				icon := boardIcon(name)
				if icon == nil {
					t.Fatal("missing icon")
				}
				for _, scale := range []float32{1, 1.25, 1.5, 2} {
					for _, dark := range []bool{false, true} {
						render := func(surface string) []byte {
							tt := ui.NewTester(func(c *ui.Context) {
								root := ui.Box(c).Fill().Background(c.Theme().Background)
								if surface == "widget" {
									root.Children(func() {
										ui.Box(c).Fill().TransformOrigin(0, 0).Scale(zoom, zoom).Children(func() {
											boardScaledIcon(c, icon, 16, c.Theme().Text, zoom).Absolute().Left(8).Top(8)
										})
									})
									return
								}
								root.Draw(func(p *ui.Painter, _ ui.Rect) {
									if surface == "canvas" {
										p.Transform(ui.Scaling(zoom, zoom), 0, 0, func() {
											paintBoardIcon(p, icon, ui.Rect{X: 8, Y: 8, W: 16, H: 16}, c.Theme().Text, zoom)
										})
									} else {
										p.Icon(icon, ui.Rect{X: 8 * zoom, Y: 8 * zoom, W: 16 * zoom, H: 16 * zoom}, c.Theme().Text)
									}
								})
							}, 128, 128)
							tt.SetScale(scale)
							tt.SetDark(dark)
							tt.Frame()
							return tt.Image().Pix
						}
						want := render("native")
						for _, surface := range []string{"canvas", "widget"} {
							if !bytes.Equal(render(surface), want) {
								t.Fatalf("zoomed %s icon differs from native size at display scale %g, dark=%v", surface, scale, dark)
							}
						}
					}
				}
			})
		}
	}
}
