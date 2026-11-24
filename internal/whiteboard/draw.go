package whiteboard

import (
	"fmt"
	"math"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard/rough"
)

// Primitive is the shared path output for native rendering, SVG and PDF.
// Coordinates are element-local; consumers apply the element transform once.
type Primitive struct {
	Ops            []rough.Op
	Fill, Stroke   string
	Width, Opacity float64
	Dash           []float64
}

func RoughOptions(e *Element) rough.Options {
	o := rough.DefaultOptions()
	o.Seed = e.Seed
	o.Stroke = e.StrokeColor
	o.StrokeWidth = e.StrokeWidth
	o.FillWeight = e.StrokeWidth / 2
	o.HachureGap = e.StrokeWidth * 4
	o.Roughness = e.Roughness
	o.PreserveVertices = e.Roughness < 2
	large, small := math.Max(e.W, e.H), math.Min(e.W, e.H)
	if !((small >= 20 && large >= 50) || (small >= 15 && e.Roundness != nil) || (e.IsLinear() && large >= 50)) {
		factor := float64(2)
		if large < 10 {
			factor = 3
		}
		o.Roughness = math.Min(e.Roughness/factor, 2.5)
	}
	if e.StrokeStyle != StrokeSolid {
		o.DisableMultiStroke = true
		o.StrokeWidth += .5
		if e.StrokeStyle == StrokeDashed {
			o.StrokeLineDash = []float64{8, 8 + e.StrokeWidth}
		} else {
			o.StrokeLineDash = []float64{1.5, 6 + e.StrokeWidth}
		}
	}
	if e.FillColor != "" && e.FillColor != "transparent" {
		o.Fill = e.FillColor
	}
	o.FillStyle = string(e.FillStyle)
	if e.Kind == KindEllipse {
		o.CurveFitting = 1
	}
	return o
}
func primitives(d rough.Drawable, opacity float64) []Primitive {
	var ps []Primitive
	for _, s := range d.Sets {
		p := Primitive{Ops: s.Ops, Opacity: opacity, Width: d.Options.StrokeWidth}
		switch s.Type {
		case "fillPath":
			p.Fill = d.Options.Fill
		case "fillSketch":
			p.Stroke = d.Options.Fill
			p.Width = d.Options.FillWeight
			if p.Width < 0 {
				p.Width = d.Options.StrokeWidth / 2
			}
		default:
			p.Stroke = d.Options.Stroke
			p.Dash = d.Options.StrokeLineDash
		}
		if p.Fill != "" || p.Stroke != "" {
			ps = append(ps, p)
		}
	}
	return ps
}
func roundedRect(w, h, r float64) string {
	return fmt.Sprintf("M %g 0 L %g 0 Q %g 0 %g %g L %g %g Q %g %g %g %g L %g %g Q 0 %g 0 %g L 0 %g Q 0 0 %g 0", r, w-r, w, w, r, w, h-r, w, h, w-r, h, r, h, h, h-r, r, r)
}
func cornerRadius(x float64, e *Element) float64 {
	if e.Roundness == nil {
		return 0
	}
	if e.Roundness.Type == 1 || e.Roundness.Type == 2 {
		return x * .25
	}
	fixed := float64(32)
	if e.Roundness.Value != nil {
		fixed = *e.Roundness.Value
	}
	return math.Min(x*.25, fixed)
}
func roundedDiamond(e *Element) string {
	tx, ty, rx, ry, bx, by, lx, ly := e.W/2, float64(0), e.W, e.H/2, e.W/2, e.H, float64(0), e.H/2
	v, h := cornerRadius(math.Abs(tx-lx), e), cornerRadius(math.Abs(ry-ty), e)
	return fmt.Sprintf("M %g %g L %g %g C %g %g %g %g %g %g L %g %g C %g %g %g %g %g %g L %g %g C %g %g %g %g %g %g L %g %g C %g %g %g %g %g %g", tx+v, ty+h, rx-v, ry-h, rx, ry, rx, ry, rx-v, ry+h, bx+v, by-h, bx, by, bx, by, bx-v, by-h, lx+v, ly+h, lx, ly, lx, ly, lx+v, ly-h, tx-v, ty+h, tx, ty, tx, ty, tx+v, ty+h)
}
func elbowPath(ps []Point) string {
	if len(ps) == 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "M %g %g ", ps[0][0], ps[0][1])
	for i := 1; i < len(ps)-1; i++ {
		p, prev, next := ps[i], ps[i-1], ps[i+1]
		r := math.Min(16, math.Min(Distance(p, prev)/2, Distance(p, next)/2))
		a := Add(p, ScalePoint(unit(Sub(prev, p)), r))
		b := Add(p, ScalePoint(unit(Sub(next, p)), r))
		fmt.Fprintf(&out, "L %g %g Q %g %g %g %g ", a[0], a[1], p[0], p[1], b[0], b[1])
	}
	last := ps[len(ps)-1]
	fmt.Fprintf(&out, "L %g %g", last[0], last[1])
	return out.String()
}
func ElementPrimitives(e *Element) []Primitive {
	o := RoughOptions(e)
	var d rough.Drawable
	opacity := e.Opacity / 100
	switch e.Kind {
	case KindText, KindImage, KindFrame, KindMagicFrame:
		return nil
	case KindNote:
		o.Roughness = 0
		o.Fill = e.FillColor
		o.FillStyle = "solid"
		o.Stroke = "none"
		d = rough.Path(roundedRect(e.W, e.H, 3), o)
	case KindRectangle, KindEmbeddable, KindIframe:
		if e.Roundness != nil {
			r := cornerRadius(math.Min(e.W, e.H), e)
			d = rough.Path(roundedRect(e.W, e.H, r), o)
		} else {
			d = rough.Rectangle(0, 0, e.W, e.H, o)
		}
	case KindDiamond:
		if e.Roundness != nil {
			d = rough.Path(roundedDiamond(e), o)
		} else {
			d = rough.Polygon([]rough.Point{{e.W / 2, 0}, {e.W, e.H / 2}, {e.W / 2, e.H}, {0, e.H / 2}}, o)
		}
	case KindEllipse:
		d = rough.Ellipse(e.W/2, e.H/2, e.W, e.H, o)
	case KindLine, KindArrow:
		ps := make([]rough.Point, len(e.Points))
		for i, p := range e.Points {
			ps[i] = rough.Point(p)
		}
		if len(ps) < 2 {
			return nil
		}
		o.Fill = ""
		if e.Polygon {
			ps = append(ps, ps[0])
			o.Fill = e.FillColor
		}
		if e.Elbowed {
			d = rough.Path(elbowPath(e.Points), o)
		} else if e.Roundness != nil && len(ps) > 2 {
			d = rough.Curve(ps, o)
		} else {
			d = rough.LinearPath(ps, o)
		}
		out := primitives(d, opacity)
		out = append(out, arrowPrimitives(e, false, o)...)
		out = append(out, arrowPrimitives(e, true, o)...)
		return out
	case KindFreedraw:
		outline := FreehandOutline(e)
		if len(outline) == 0 {
			return nil
		}
		ops := []rough.Op{{Op: "move", Data: []float64{outline[0][0], outline[0][1]}}}
		for i, p := range outline {
			next := outline[(i+1)%len(outline)]
			mid := Lerp(p, next, .5)
			ops = append(ops, rough.Op{Op: "quadTo", Data: []float64{p[0], p[1], mid[0], mid[1]}})
		}
		ops = append(ops, rough.Op{Op: "close"})
		out := []Primitive{}
		if len(e.Points) > 2 && Distance(e.Points[0], e.Points[len(e.Points)-1]) < 10 && o.Fill != "" {
			ps := make([]rough.Point, len(e.Points))
			for i, p := range e.Points {
				ps[i] = rough.Point(p)
			}
			out = append(out, primitives(rough.Polygon(ps, o), opacity)...)
		}
		return append(out, Primitive{Ops: ops, Fill: e.StrokeColor, Opacity: opacity})
	default:
		return nil
	}
	return primitives(d, opacity)
}
func arrowPrimitives(e *Element, end bool, o rough.Options) []Primitive {
	head := e.StartArrowhead
	if end {
		head = e.EndArrowhead
	}
	if head == "" || len(e.Points) < 2 {
		return nil
	}
	p, q := e.Points[0], e.Points[1]
	if end {
		p, q = e.Points[len(e.Points)-1], e.Points[len(e.Points)-2]
	}
	v := unit(Sub(q, p))
	if v == (Point{}) {
		return nil
	}
	n := perp(v)
	size := math.Min(25, math.Max(10, e.StrokeWidth*6))
	base := Add(p, ScalePoint(v, size))
	left, right := Add(base, ScalePoint(n, size*.45)), Sub(base, ScalePoint(n, size*.45))
	o.Fill = ""
	o.Roughness = 0
	o.DisableMultiStroke = true
	o.PreserveVertices = true
	var ds []rough.Drawable
	line := func(a, b Point) { ds = append(ds, rough.Line(a[0], a[1], b[0], b[1], o)) }
	bar := func(at Point) { line(Add(at, ScalePoint(n, size*.4)), Sub(at, ScalePoint(n, size*.4))) }
	switch head {
	case ArrowOpen:
		line(left, p)
		line(p, right)
	case ArrowBar:
		bar(p)
	case ArrowTriangle, ArrowTriangleOutline:
		o.Fill = e.StrokeColor
		if head == ArrowTriangleOutline {
			o.Fill = "#ffffff"
		}
		o.FillStyle = "solid"
		ds = append(ds, rough.Polygon([]rough.Point{rough.Point(p), rough.Point(left), rough.Point(right)}, o))
	case ArrowDiamond, ArrowDiamondOutline:
		o.Fill = e.StrokeColor
		if head == ArrowDiamondOutline {
			o.Fill = "#ffffff"
		}
		o.FillStyle = "solid"
		ds = append(ds, rough.Polygon([]rough.Point{rough.Point(p), rough.Point(Add(Add(p, ScalePoint(v, size*.5)), ScalePoint(n, size*.35))), rough.Point(base), rough.Point(Sub(Add(p, ScalePoint(v, size*.5)), ScalePoint(n, size*.35)))}, o))
	case ArrowCircle, ArrowCircleOutline:
		o.Fill = e.StrokeColor
		if head == ArrowCircleOutline {
			o.Fill = "#ffffff"
		}
		o.FillStyle = "solid"
		ds = append(ds, rough.Ellipse(p[0], p[1], size*.7, size*.7, o))
	default:
		if strings.Contains(string(head), "many") {
			line(left, p)
			line(right, p)
			line(base, p)
		}
		if strings.Contains(string(head), "one") {
			bar(Add(p, ScalePoint(v, size*.6)))
			if head == "cardinality_exactly_one" {
				bar(Add(p, ScalePoint(v, size)))
			}
		}
		if strings.Contains(string(head), "zero") {
			c := Add(p, ScalePoint(v, size*1.15))
			o.Fill = "#ffffff"
			o.FillStyle = "solid"
			ds = append(ds, rough.Ellipse(c[0], c[1], size*.55, size*.55, o))
		}
	}
	var out []Primitive
	for _, d := range ds {
		out = append(out, primitives(d, e.Opacity/100)...)
	}
	return out
}
func PathData(ops []rough.Op) string {
	var b strings.Builder
	for _, o := range ops {
		switch o.Op {
		case "move":
			fmt.Fprintf(&b, "M %g %g ", o.Data[0], o.Data[1])
		case "lineTo":
			fmt.Fprintf(&b, "L %g %g ", o.Data[0], o.Data[1])
		case "bcurveTo":
			fmt.Fprintf(&b, "C %g %g %g %g %g %g ", o.Data[0], o.Data[1], o.Data[2], o.Data[3], o.Data[4], o.Data[5])
		case "quadTo":
			fmt.Fprintf(&b, "Q %g %g %g %g ", o.Data[0], o.Data[1], o.Data[2], o.Data[3])
		case "close":
			b.WriteString("Z ")
		}
	}
	return b.String()
}
