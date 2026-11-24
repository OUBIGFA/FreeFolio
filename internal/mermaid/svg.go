package mermaid

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// Style gives an exported diagram its colors as CSS color values: the
// caller picks them for each primitive the way it paints it on screen, so
// the picture matches the editor and this package keeps its distance from
// any UI toolkit, like the rest of internal/mermaid. Every field must be
// set. Adapted from ArcMD 8f593e5, whose fixed palette knew only flowcharts
// and sequence diagrams; FreeFolio also draws gantt charts, pies and charts.
type Style struct {
	// Paper is the picture's background, behind pills and inside hollow
	// markers.
	Paper string
	// Shape is a shape's fill, its outline and the outline's width; a zero
	// width draws no outline.
	Shape func(ShapeOp) (fill, stroke string, width float64)
	// Path is a path's stroke and width.
	Path func(PathOp) (stroke string, width float64)
	// Label is a label's color.
	Label func(LabelOp) string
	// Back reports the shapes drawn behind the paths.
	Back func(ShapeOp) bool
}

// FontFamily is the family an exported picture and the measurement that
// sized its boxes share. The rest of svgFontStack only supplies fallbacks for
// platforms without a system UI family.
const FontFamily = "system-ui"

// svgFontStack is what the picture declares: the measured family first, then
// the page's fallbacks.
const svgFontStack = FontFamily + `, -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif`

// svgPadding keeps glyphs that measure a little narrower than the browser's
// font inside the picture.
const svgPadding = 4

// ExportSVG draws d as a standalone SVG document on the palette's paper. alt
// names the picture for screen readers and is omitted when empty. The drawing
// order matches the editor: containers behind edges, edges behind nodes,
// labels on top. Every Palette field is a CSS color and must be set.
func ExportSVG(d Diagram, st Style, alt string) string {
	w, h := d.W, d.H
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	w, h = w+2*svgPadding, h+2*svgPadding
	aria := ""
	if strings.TrimSpace(alt) != "" {
		aria = ` aria-label="` + html.EscapeString(alt) + `"`
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.2f %.2f" role="img"%s font-family="%s">`,
		math.Ceil(float64(w)), math.Ceil(float64(h)), w, h, aria, svgFontStack)
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%.2f" height="%.2f" fill="%s"/>`, w, h, st.Paper)
	fmt.Fprintf(&b, `<g transform="translate(%.1f,%.1f)">`, float32(svgPadding), float32(svgPadding))
	for _, s := range d.Shapes {
		if st.Back(s) {
			writeShape(&b, s, st)
		}
	}
	for _, p := range d.Paths {
		writePath(&b, p, st)
	}
	for _, s := range d.Shapes {
		if !st.Back(s) {
			writeShape(&b, s, st)
		}
	}
	for _, l := range d.Labels {
		writeLabel(&b, l, st)
	}
	b.WriteString("</g></svg>")
	return b.String()
}

func writeShape(b *strings.Builder, s ShapeOp, st Style) {
	if s.Kind == ShapePolygon {
		if len(s.Points) < 3 {
			return
		}
	} else if s.W <= 0 || s.H <= 0 {
		return
	}
	fill, stroke, width := st.Shape(s)
	x, y, w, h := s.X, s.Y, s.W, s.H
	attrs := fmt.Sprintf(`fill="%s" stroke="%s" stroke-width="%.2f"`, fill, stroke, width)
	if width <= 0 {
		attrs = fmt.Sprintf(`fill="%s" stroke="none"`, fill)
	}
	switch s.Kind {
	case ShapePolygon:
		// Pie slices and chart areas.
		var d strings.Builder
		for i, pt := range s.Points {
			op := "L"
			if i == 0 {
				op = "M"
			}
			fmt.Fprintf(&d, "%s%.2f %.2f ", op, pt[0], pt[1])
		}
		fmt.Fprintf(b, `<path d="%sZ" %s stroke-linejoin="round"/>`, d.String(), attrs)
	case ShapeRect:
		writeRect(b, x, y, w, h, s.Radius, attrs, s.Dashed)
	case ShapeRound:
		writeRect(b, x, y, w, h, max(s.Radius, 6), attrs, s.Dashed)
	case ShapeStadium:
		writeRect(b, x, y, w, h, min(w, h)/2, attrs, false)
	case ShapeCircle:
		fmt.Fprintf(b, `<circle cx="%.2f" cy="%.2f" r="%.2f" %s/>`, x+w/2, y+h/2, min(w, h)/2, attrs)
	case ShapeSubroutine:
		writeRect(b, x, y, w, h, 4, attrs, false)
		inset := min(8, w*0.14)
		fmt.Fprintf(b, `<path d="M%.2f %.2f V%.2f M%.2f %.2f V%.2f" fill="none" stroke="%s" stroke-width="%.2f"/>`,
			x+inset, y, y+h, x+w-inset, y, y+h, stroke, width)
	case ShapeCylinder:
		ry := min(7, h*0.18)
		kx := w * 0.28
		d := fmt.Sprintf("M%.2f %.2f L%.2f %.2f C%.2f %.2f %.2f %.2f %.2f %.2f C%.2f %.2f %.2f %.2f %.2f %.2f L%.2f %.2f C%.2f %.2f %.2f %.2f %.2f %.2f C%.2f %.2f %.2f %.2f %.2f %.2f Z",
			x, y+ry, x, y+h-ry,
			x, y+h-ry+ry*0.9, x+kx, y+h, x+w/2, y+h,
			x+w-kx, y+h, x+w, y+h-ry+ry*0.9, x+w, y+h-ry,
			x+w, y+ry,
			x+w, y+ry-ry*0.9, x+w-kx, y, x+w/2, y,
			x+kx, y, x, y+ry-ry*0.9, x, y+ry)
		fmt.Fprintf(b, `<path d="%s" %s/>`, d, attrs)
		rim := fmt.Sprintf("M%.2f %.2f C%.2f %.2f %.2f %.2f %.2f %.2f C%.2f %.2f %.2f %.2f %.2f %.2f",
			x, y+ry,
			x, y+ry+ry*0.9, x+kx, y+2*ry, x+w/2, y+2*ry,
			x+w-kx, y+2*ry, x+w, y+ry+ry*0.9, x+w, y+ry)
		fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="%.2f"/>`, rim, stroke, width)
	case ShapeDiamond:
		cx, cy := x+w/2, y+h/2
		fmt.Fprintf(b, `<path d="M%.2f %.2f L%.2f %.2f L%.2f %.2f L%.2f %.2f Z" %s/>`,
			cx, y, x+w, cy, cx, y+h, x, cy, attrs)
	case ShapeHexagon:
		inset := min(14, w*0.18)
		cy := y + h/2
		fmt.Fprintf(b, `<path d="M%.2f %.2f L%.2f %.2f L%.2f %.2f L%.2f %.2f L%.2f %.2f L%.2f %.2f Z" %s/>`,
			x+inset, y, x+w-inset, y, x+w, cy, x+w-inset, y+h, x+inset, y+h, x, cy, attrs)
	case ShapeParallelogram:
		skew := min(14, w*0.18)
		fmt.Fprintf(b, `<path d="M%.2f %.2f L%.2f %.2f L%.2f %.2f L%.2f %.2f Z" %s/>`,
			x+skew, y, x+w, y, x+w-skew, y+h, x, y+h, attrs)
	case ShapePentagon:
		cut := min(7, min(w, h)*0.35)
		dashed := ""
		if s.Dashed {
			dashed = ` stroke-dasharray="5 4"`
		}
		fmt.Fprintf(b, `<path d="M%.2f %.2f L%.2f %.2f L%.2f %.2f L%.2f %.2f L%.2f %.2f Z" %s%s/>`,
			x, y, x+w, y, x+w, y+h-cut, x+w-cut, y+h, x, y+h, attrs, dashed)
	case ShapeActor:
		cx := x + w/2
		headR := min(w, h) * 0.22
		headY := y + headR + 1
		fmt.Fprintf(b, `<circle cx="%.2f" cy="%.2f" r="%.2f" %s/>`, cx, headY, headR, attrs)
		neckY := headY + headR
		hipY := y + h*0.66
		armY := y + h*0.44
		fmt.Fprintf(b, `<path d="M%.2f %.2f V%.2f M%.2f %.2f H%.2f M%.2f %.2f L%.2f %.2f M%.2f %.2f L%.2f %.2f" fill="none" stroke="%s" stroke-width="%.2f"/>`,
			cx, neckY, hipY,
			cx-headR*1.5, armY, cx+headR*1.5,
			cx, hipY, cx-headR*1.2, y+h-1,
			cx, hipY, cx+headR*1.2, y+h-1,
			stroke, width)
	}
}

func writeRect(b *strings.Builder, x, y, w, h, radius float32, attrs string, dashed bool) {
	dash := ""
	if dashed {
		dash = ` stroke-dasharray="4 3"`
	}
	fmt.Fprintf(b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="%.2f" %s%s/>`,
		x, y, w, h, radius, attrs, dash)
}

func writePath(b *strings.Builder, op PathOp, st Style) {
	if len(op.Points) < 2 {
		return
	}
	pts := op.Points
	col, width := st.Path(op)

	// Shorten the shaft slightly at a filled arrowhead so the blunt cap does
	// not poke past its tip, as the editor does.
	drawPts := append([][2]float32(nil), pts...)
	if op.StartMarker == MarkerArrow {
		if ux, uy, d := segDirLen(pts[1], pts[0]); d >= 8 {
			drawPts[0] = [2]float32{pts[0][0] - ux*5, pts[0][1] - uy*5}
		}
	}
	if op.EndMarker == MarkerArrow {
		n := len(drawPts)
		if ux, uy, d := segDirLen(drawPts[n-2], drawPts[n-1]); d >= 8 {
			drawPts[n-1] = [2]float32{pts[n-1][0] - ux*5, pts[n-1][1] - uy*5}
		}
	}

	var d strings.Builder
	fmt.Fprintf(&d, "M%.2f %.2f", drawPts[0][0], drawPts[0][1])
	if op.Smooth && len(drawPts) >= 3 {
		for i := 1; i < len(drawPts)-1; i++ {
			prev, cur, next := drawPts[i-1], drawPts[i], drawPts[i+1]
			ux1, uy1, d1 := segDirLen(prev, cur)
			ux2, uy2, d2 := segDirLen(cur, next)
			if d1 < 2 || d2 < 2 {
				fmt.Fprintf(&d, " L%.2f %.2f", cur[0], cur[1])
				continue
			}
			rad := min(8, min(d1, d2)*0.35)
			fmt.Fprintf(&d, " L%.2f %.2f Q%.2f %.2f %.2f %.2f",
				cur[0]-ux1*rad, cur[1]-uy1*rad, cur[0], cur[1], cur[0]+ux2*rad, cur[1]+uy2*rad)
		}
		last := drawPts[len(drawPts)-1]
		fmt.Fprintf(&d, " L%.2f %.2f", last[0], last[1])
	} else {
		for _, pt := range drawPts[1:] {
			fmt.Fprintf(&d, " L%.2f %.2f", pt[0], pt[1])
		}
	}
	dash := ""
	if op.Style == EdgeDashed {
		dash = ` stroke-dasharray="5 4"`
	}
	fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round"%s/>`,
		d.String(), col, width, dash)

	if op.StartMarker != MarkerNone {
		if ux, uy, ok := segUnit(pts[1], pts[0]); ok {
			writeMarker(b, op.StartMarker, pts[0][0], pts[0][1], ux, uy, float32(width), col, st.Paper)
		}
	}
	if op.EndMarker != MarkerNone {
		n := len(pts)
		if ux, uy, ok := segUnit(pts[n-2], pts[n-1]); ok {
			writeMarker(b, op.EndMarker, pts[n-1][0], pts[n-1][1], ux, uy, float32(width), col, st.Paper)
		}
	}
}

func writeMarker(b *strings.Builder, m MarkerKind, tipX, tipY, ux, uy, strokeW float32, col, paper string) {
	px, py := -uy, ux
	switch m {
	case MarkerArrow:
		size := float32(9)
		halfW := float32(4.2)
		fmt.Fprintf(b, `<path d="M%.2f %.2f L%.2f %.2f L%.2f %.2f Z" fill="%s"/>`,
			tipX, tipY,
			tipX-ux*size+px*halfW, tipY-uy*size+py*halfW,
			tipX-ux*size-px*halfW, tipY-uy*size-py*halfW,
			col)
	case MarkerOpenArrow:
		size := float32(8.5)
		halfW := float32(4.5)
		fmt.Fprintf(b, `<path d="M%.2f %.2f L%.2f %.2f L%.2f %.2f" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round"/>`,
			tipX-ux*size+px*halfW, tipY-uy*size+py*halfW,
			tipX, tipY,
			tipX-ux*size-px*halfW, tipY-uy*size-py*halfW,
			col, max(1.3, strokeW))
	case MarkerCircle:
		r := float32(3.8)
		fmt.Fprintf(b, `<circle cx="%.2f" cy="%.2f" r="%.2f" fill="%s" stroke="%s" stroke-width="%.2f"/>`,
			tipX-ux*r, tipY-uy*r, r, paper, col, max(1.3, strokeW))
	case MarkerCross:
		r := float32(3.8)
		cx, cy := tipX-ux*5, tipY-uy*5
		dx1, dy1 := (ux+px)*r*0.75, (uy+py)*r*0.75
		dx2, dy2 := (ux-px)*r*0.75, (uy-py)*r*0.75
		fmt.Fprintf(b, `<path d="M%.2f %.2f L%.2f %.2f M%.2f %.2f L%.2f %.2f" stroke="%s" stroke-width="%.2f" stroke-linecap="round"/>`,
			cx-dx1, cy-dy1, cx+dx1, cy+dy1,
			cx-dx2, cy-dy2, cx+dx2, cy+dy2,
			col, max(1.4, strokeW))
	}
}

func writeLabel(b *strings.Builder, l LabelOp, st Style) {
	if strings.TrimSpace(l.Text) == "" || l.Size <= 0 {
		return
	}
	fg := st.Label(l)
	if l.Pill {
		fmt.Fprintf(b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="4" fill="%s" fill-opacity="0.92"/>`,
			l.X-4, l.Y-1, l.W+8, l.H+2, st.Paper)
	}
	lines := strings.Split(l.Text, "\n")
	lh := l.Size * 1.35
	top := l.Y + (l.H-float32(len(lines))*lh)/2
	x := l.X + l.W/2
	anchor := "middle"
	switch l.Align {
	case -1:
		x, anchor = l.X, "start"
	case 1:
		x, anchor = l.X+l.W, "end"
	}
	weight := 400
	if l.Bold {
		weight = 600
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		y := top + float32(i)*lh + l.Size*0.97
		fmt.Fprintf(b, `<text x="%.2f" y="%.2f" font-size="%.1f" font-weight="%d" text-anchor="%s" fill="%s">%s</text>`,
			x, y, l.Size, weight, anchor, fg, html.EscapeString(line))
	}
}

func segDirLen(a, b [2]float32) (ux, uy, dist float32) {
	dx := b[0] - a[0]
	dy := b[1] - a[1]
	d := float32(math.Hypot(float64(dx), float64(dy)))
	if d < 0.001 {
		return 0, 0, 0
	}
	return dx / d, dy / d, d
}

func segUnit(a, b [2]float32) (ux, uy float32, ok bool) {
	u, v, d := segDirLen(a, b)
	return u, v, d >= 0.001
}
