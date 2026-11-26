package editor

import (
	"bytes"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestBoardLinkAtDisplayedScale(t *testing.T) {
	curve := [4]whiteboard.Point{{4, 4}, {12, 2}, {9, 18}, {24, 14}}
	board := NewBoard(whiteboard.NewScene())
	board.Zoom = 4
	render := func(zoomed bool) []byte {
		tester := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, ui.RGB(255, 255, 255), 0)
				ink := ui.RGB(9, 48, 80)
				if zoomed {
					p.Transform(ui.Scaling(4, 4), 0, 0, func() { board.paintAILink(p, curve, ink, 1, 4) })
				} else {
					path := new(ui.Path).Cap(ui.RoundCap).MoveTo(16, 16).CubeTo(48, 8, 36, 72, 96, 56)
					p.StrokePath(path, 4, ink)
				}
			})
		}, 128, 104)
		tester.Frame()
		picture := tester.Image()
		var sample []byte
		for row := 0; row < 80; row++ {
			sample = append(sample, picture.Pix[row*picture.Stride:row*picture.Stride+40*4]...)
		}
		return sample
	}
	if !bytes.Equal(render(true), render(false)) {
		t.Fatal("zoomed link differs from the same curve drawn at display resolution")
	}
}

func TestBoardVectorShapeAtDisplayedScale(t *testing.T) {
	render := func(drawZoom float32) []byte {
		board := NewBoard(whiteboard.NewScene())
		size := float64(4 / drawZoom)
		shape := whiteboard.NewElement(whiteboard.KindDiamond, 3*size, 3*size, 17*size, 19*size)
		shape.Roundness = nil
		shape.Seed = 11
		shape.Roughness = 0
		shape.StrokeWidth = 1.5 * size
		shape.FillColor = "#7788aa"
		shape.FillStyle = whiteboard.FillSolid
		tester := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, ui.RGB(255, 255, 255), 0)
				p.Transform(ui.Scaling(drawZoom, drawZoom), 0, 0, func() { board.paintElement(p, shape, false, drawZoom) })
			})
		}, 112, 116)
		tester.SetScale(1.5)
		tester.Frame()
		return tester.Image().Pix
	}
	if !bytes.Equal(render(4), render(1)) {
		t.Fatal("zoomed shape differs from the same vector drawn at physical resolution")
	}
}
