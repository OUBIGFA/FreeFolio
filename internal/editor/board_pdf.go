package editor

import (
	"bytes"
	"image"
	"math"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/pdf"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func (e *Editor) ExportError() error { return e.exportErr }

func RegisterBoardPDFFonts(w *pdf.Writer, s *whiteboard.Scene) {
	seen := map[int]bool{}
	for _, e := range s.Elements {
		if e.Kind != whiteboard.KindText || seen[e.FontFamily] {
			continue
		}
		id := e.FontFamily
		seen[id] = true
		family := pdf.FontFamily(16 + id)
		if data, err := BoardFontData(id); err == nil {
			w.RegisterFontData(family, false, false, data)
		}
		if id == 5 {
			if data, err := BoardFontData(100); err == nil {
				w.RegisterFontData(family, false, false, data)
			}
		}
	}
}
func boardPDFColor(value string, opacity float64) [4]uint8 {
	c := boardColor(value, false)
	return [4]uint8{c.R, c.G, c.B, uint8(math.Round(float64(c.A) * opacity))}
}

// ExportBoardVector shares paths with the interactive renderer. Text remains
// selectable Type 3 glyphs; only source image elements are raster content.
func ExportBoardVector(vp *pdf.VectorPage, s *whiteboard.Scene, dst whiteboard.Box, o BoardOptions) error {
	bounds := whiteboard.SceneBounds(s).Expand(16)
	if bounds.Empty() {
		return nil
	}
	k := math.Min(dst.W/bounds.W, dst.H/bounds.H)
	ox := dst.X + (dst.W-bounds.W*k)/2 - bounds.X*k
	oy := dst.Y + (dst.H-bounds.H*k)/2 - bounds.Y*k
	for _, e := range s.Elements {
		if e.IsDeleted {
			continue
		}
		frame := s.ElementByID(e.FrameID)
		if frame != nil {
			var ps [][2]float64
			for _, p := range whiteboard.WorldPoints(frame) {
				ps = append(ps, [2]float64{ox + p[0]*k, oy + p[1]*k})
			}
			vp.PushClipPolygon(ps)
		}
		vp.Save()
		sin, cos := math.Sincos(e.Angle)
		cx, cy := e.W/2, e.H/2
		tx := ox + k*(e.X+cx-cx*cos+cy*sin)
		ty := oy + k*(e.Y+cy-cx*sin-cy*cos)
		vp.Transform(k*cos, k*sin, -k*sin, k*cos, tx, ty)
		for _, p := range whiteboard.ElementPrimitives(e) {
			var segs []pdf.PathSegment
			for _, op := range p.Ops {
				d := op.Data
				seg := pdf.PathSegment{}
				switch op.Op {
				case "move":
					seg.Op = 0
					seg.X0, seg.Y0 = d[0], d[1]
				case "lineTo":
					seg.Op = 1
					seg.X0, seg.Y0 = d[0], d[1]
				case "quadTo":
					seg.Op = 2
					seg.X0, seg.Y0, seg.X1, seg.Y1 = d[0], d[1], d[2], d[3]
				case "bcurveTo":
					seg.Op = 3
					seg.X0, seg.Y0, seg.X1, seg.Y1, seg.X2, seg.Y2 = d[0], d[1], d[2], d[3], d[4], d[5]
				case "close":
					seg.Op = 4
				}
				segs = append(segs, seg)
			}
			vp.DrawPath(segs, boardPDFColor(p.Fill, p.Opacity), boardPDFColor(p.Stroke, p.Opacity), p.Width, len(p.Dash) > 0)
		}
		switch e.Kind {
		case whiteboard.KindText:
			if host := s.ElementByID(e.ContainerID); host != nil && host.Kind == whiteboard.KindArrow {
				vp.FillRect(-4, -2, e.W+8, e.H+4, 2, boardPDFColor(s.AppState.ViewBackgroundColor, 1))
			}
			lh := e.FontSize * e.LineHeight
			if lh <= 0 {
				lh = e.FontSize * whiteboard.FontLineHeight(e.FontFamily)
			}
			for i, line := range strings.Split(e.Text, "\n") {
				w := boardMeasure(line, whiteboard.Font{Size: e.FontSize, Family: e.FontFamily})
				x := float64(0)
				if e.TextAlign == "center" {
					x = (e.W - w) / 2
				} else if e.TextAlign == "right" {
					x = e.W - w
				}
				vp.DrawText(line, x, whiteboard.TextBaseline(e.FontFamily, e.FontSize, lh)+float64(i)*lh, w, e.FontSize, pdf.TextStyle{Family: pdf.FontFamily(16 + e.FontFamily)}, boardPDFColor(e.StrokeColor, e.Opacity/100))
			}
		case whiteboard.KindImage:
			data, err := boardFileBytes(s.Files[e.FileID], o)
			if err != nil {
				return err
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return err
			}
			vp.PushClip(0, 0, e.W, e.H, 0)
			sx, sy := e.Scale[0], e.Scale[1]
			if sx == 0 {
				sx = 1
			}
			if sy == 0 {
				sy = 1
			}
			fx, fy := float64(0), float64(0)
			if sx < 0 {
				fx = e.W
			}
			if sy < 0 {
				fy = e.H
			}
			vp.Transform(sx, 0, 0, sy, fx, fy)
			x, y, w, h := float64(0), float64(0), e.W, e.H
			if c := e.Crop; c != nil {
				x = -c.X * e.W / c.Width
				y = -c.Y * e.H / c.Height
				w = c.NaturalWidth * e.W / c.Width
				h = c.NaturalHeight * e.H / c.Height
			}
			vp.DrawImage(img, x, y, w, h, 0)
			vp.PopClip()
		case whiteboard.KindFrame, whiteboard.KindMagicFrame:
			vp.StrokeRect(0, 0, e.W, e.H, 0, 1, boardPDFColor("#adb5bd", 1), false)
			vp.DrawText(e.Name, 0, -8, 0, 14, pdf.TextStyle{Family: pdf.FontSans}, boardPDFColor("#868e96", 1))
		}
		vp.Restore()
		if frame != nil {
			vp.PopClip()
		}
	}
	return nil
}
