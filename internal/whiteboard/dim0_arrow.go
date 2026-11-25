package whiteboard

import (
	"math"
	"slices"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard/rough"
)

// DimArrowhead ports canvas-harness 0.2.1 edges/arrowhead.ts. Direction is the
// unit vector toward the tip. Filled heads deliberately have no extra outline.
func DimArrowhead(kind string, tip, direction Point, width float64, color string, opacity float64) Primitive {
	scale := math.Max(1, width/2)
	length, half := 12*scale, 4*scale
	base := Sub(tip, ScalePoint(direction, length))
	perpendicular := Point{-direction[1], direction[0]}
	left, right := Add(base, ScalePoint(perpendicular, half)), Sub(base, ScalePoint(perpendicular, half))
	op := func(name string, p Point) rough.Op { return rough.Op{Op: name, Data: []float64{p[0], p[1]}} }
	result := Primitive{Width: width, Opacity: opacity}
	switch kind {
	case "arrow":
		result.Stroke = color
		result.Ops = []rough.Op{op("move", left), op("lineTo", tip), op("lineTo", right)}
	case "arrow-filled", "triangle":
		result.Fill = color
		result.Ops = []rough.Op{op("move", tip), op("lineTo", left), op("lineTo", right), {Op: "close"}}
	case "barb":
		notch := Add(base, ScalePoint(direction, length*.35))
		result.Fill = color
		result.Ops = []rough.Op{op("move", tip), op("lineTo", left), op("lineTo", notch), op("lineTo", right), {Op: "close"}}
	}
	return result
}

// DimArrowPoints shares the sampled geometry between paint, hit testing and
// bounds, as canvas-harness does. A third point is a draggable curve midpoint.
func DimArrowPoints(e *Element) []Point {
	if e.Roundness == nil || e.Elbowed || len(e.Points) < 2 {
		return e.Points
	}
	if len(e.Points) > 3 {
		// FreeFolio also permits multi-point paths. Apply dim0's midpoint
		// curve to consecutive spans so switching style never drops a point.
		var out []Point
		i := 0
		for ; i+2 < len(e.Points); i += 2 {
			span := *e
			span.Points = e.Points[i : i+3]
			points := DimArrowPoints(&span)
			if len(out) > 0 {
				points = points[1:]
			}
			out = append(out, points...)
		}
		if i < len(e.Points)-1 {
			out = append(out, e.Points[len(e.Points)-1])
		}
		return out
	}
	a, z := e.Points[0], e.Points[len(e.Points)-1]
	offset := math.Min(200, Distance(a, z)*.4)
	normal := func(binding *Binding, fallback Point) Point {
		if binding == nil {
			return fallback
		}
		p := binding.FixedPoint
		d := math.Min(math.Min(p[0], 1-p[0]), math.Min(p[1], 1-p[1]))
		switch d {
		case p[0]:
			return Point{-1, 0}
		case 1 - p[0]:
			return Point{1, 0}
		case p[1]:
			return Point{0, -1}
		default:
			return Point{0, 1}
		}
	}
	c1, c2 := Add(a, ScalePoint(normal(e.StartBinding, unit(Sub(z, a))), offset)), Add(z, ScalePoint(normal(e.EndBinding, unit(Sub(a, z))), offset))
	if len(e.Points) > 2 {
		mid := e.Points[len(e.Points)/2]
		c1 = ScalePoint(Sub(Sub(ScalePoint(mid, 8), a), z), 1.0/6)
		c2 = c1
	}
	out := make([]Point, 33)
	for i := range out {
		t := float64(i) / 32
		u := 1 - t
		out[i] = Add(Add(ScalePoint(a, u*u*u), ScalePoint(c1, 3*u*u*t)), Add(ScalePoint(c2, 3*u*t*t), ScalePoint(z, t*t*t)))
	}
	return out
}

func dimArrowPrimitives(e *Element) []Primitive {
	if e.StrokeWidth <= 0 {
		return nil
	}
	points := DimArrowPoints(e)
	if len(points) < 2 {
		return nil
	}
	opacity := e.Opacity / 100
	var heads []Primitive
	line := slices.Clone(points)
	for _, end := range []bool{false, true} {
		head := e.StartArrowhead
		i, j := 0, 1
		if end {
			head = e.EndArrowhead
			i, j = len(points)-1, len(points)-2
		}
		if head == "" {
			continue
		}
		switch head {
		case ArrowOpen, ArrowTriangle, "barb":
			p := DimArrowhead(string(head), points[i], unit(Sub(points[i], points[j])), e.StrokeWidth, e.StrokeColor, opacity)
			heads = append(heads, p)
			// Pull the body into the head. Use the same near-tip segment rule
			// as canvas-harness so short and long curves match its end caps.
			length := 12 * math.Max(1, e.StrokeWidth/2)
			v := Sub(points[i], points[j])
			n := Distance(points[i], points[j])
			if n >= 1e-6 {
				line[i] = Sub(points[i], ScalePoint(v, length/n*math.Min(1, length/n)))
			}
		default:
			copy := *e
			copy.Points = points
			heads = append(heads, arrowPrimitives(&copy, end, RoughOptions(e))...)
		}
	}
	ops := []rough.Op{{Op: "move", Data: []float64{line[0][0], line[0][1]}}}
	for _, p := range line[1:] {
		ops = append(ops, rough.Op{Op: "lineTo", Data: []float64{p[0], p[1]}})
	}
	body := Primitive{Ops: ops, Stroke: e.StrokeColor, Width: e.StrokeWidth, Opacity: opacity}
	if e.Roughness > 0 && e.StrokeStyle == StrokeSolid {
		ring := dimBrushOutline(line, e.StrokeWidth)
		if len(ring) > 0 {
			body.Stroke, body.Fill = "", e.StrokeColor
			body.Ops = []rough.Op{{Op: "move", Data: []float64{ring[0][0], ring[0][1]}}}
			for i, p := range ring {
				mid := Lerp(p, ring[(i+1)%len(ring)], .5)
				body.Ops = append(body.Ops, rough.Op{Op: "quadTo", Data: []float64{p[0], p[1], mid[0], mid[1]}})
			}
			body.Ops = append(body.Ops, rough.Op{Op: "close"})
		}
	}
	if e.StrokeStyle == StrokeDashed {
		body.Dash = []float64{8, 8 + e.StrokeWidth}
	} else if e.StrokeStyle == StrokeDotted {
		body.Dash = []float64{1.5, 6 + e.StrokeWidth}
	}
	return append([]Primitive{body}, heads...)
}

// dimBrushOutline ports the pressure bell and stroke options of dim0's
// edges/freehand.ts. Tapers extend the existing perfect-freehand port without
// changing the whiteboard pen's untapered stroke behavior.
func dimBrushOutline(points []Point, width float64) []Point {
	in := make([]strokeInput, len(points))
	for i, p := range points {
		t := float64(i) / float64(max(1, len(points)-1))
		in[i] = strokeInput{p: p, pressure: .35 + .55*math.Sin(math.Pi*t), hasPressure: true}
	}
	size := math.Max(1.2, width) * 1.3
	o := freehandOptions{size: size, thinning: .55, smoothing: .6, streamline: .55, taper: math.Min(24, size*3), easing: func(t float64) float64 { return t }}
	return strokeOutline(strokePoints(in, o), o)
}
