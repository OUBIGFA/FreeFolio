// Package rough ports rough.js 4.6.4 through Quiet.md e59c49a (MIT,
// Copyright Preet Shihn and s1dashu). Random draws retain upstream order.
package rough

import "math"

type Point = [2]float64
type Op struct {
	Op   string
	Data []float64
}
type Set struct {
	Type string
	Ops  []Op
}
type Drawable struct {
	Sets    []Set
	Options Options
}
type Options struct {
	MaxRandomnessOffset    float64   `json:"maxRandomnessOffset"`
	Roughness              float64   `json:"roughness"`
	Bowing                 float64   `json:"bowing"`
	Stroke                 string    `json:"stroke"`
	StrokeWidth            float64   `json:"strokeWidth"`
	CurveTightness         float64   `json:"curveTightness"`
	CurveFitting           float64   `json:"curveFitting"`
	CurveStepCount         float64   `json:"curveStepCount"`
	FillStyle              string    `json:"fillStyle"`
	FillWeight             float64   `json:"fillWeight"`
	HachureAngle           float64   `json:"hachureAngle"`
	HachureGap             float64   `json:"hachureGap"`
	DashOffset             float64   `json:"dashOffset"`
	DashGap                float64   `json:"dashGap"`
	ZigzagOffset           float64   `json:"zigzagOffset"`
	Seed                   int64     `json:"seed"`
	DisableMultiStroke     bool      `json:"disableMultiStroke"`
	DisableMultiStrokeFill bool      `json:"disableMultiStrokeFill"`
	PreserveVertices       bool      `json:"preserveVertices"`
	FillShapeRoughnessGain float64   `json:"fillShapeRoughnessGain"`
	Fill                   string    `json:"fill"`
	Simplification         *float64  `json:"simplification"`
	StrokeLineDash         []float64 `json:"strokeLineDash"`
	StrokeLineDashOffset   float64   `json:"strokeLineDashOffset"`
	FillLineDash           []float64 `json:"fillLineDash"`
	FillLineDashOffset     float64   `json:"fillLineDashOffset"`
}

func DefaultOptions() Options {
	return Options{MaxRandomnessOffset: 2, Roughness: 1, Bowing: 1, Stroke: "#000", StrokeWidth: 1, CurveFitting: .95, CurveStepCount: 9, FillStyle: "hachure", FillWeight: -1, HachureAngle: -41, HachureGap: -1, DashOffset: -1, DashGap: -1, ZigzagOffset: -1, FillShapeRoughnessGain: .8}
}

type random struct {
	state   int32
	math    uint32
	started bool
}

func newRandom(seed int64) *random { return &random{state: int32(seed), math: uint32(seed)} }
func (r *random) next() float64 {
	r.started = true
	if r.state == 0 {
		return r.mathRandom()
	}
	r.state *= 48271
	return float64(r.state&0x7fffffff) / 2147483648
}
func (r *random) mathRandom() float64 {
	r.math += 0x6d2b79f5
	n := r.math
	n = (n ^ (n >> 15)) * (n | 1)
	n ^= n + (n^(n>>7))*(n|61)
	return float64(n^(n>>14)) / 4294967296
}
func move(p Point) Op        { return Op{"move", []float64{p[0], p[1]}} }
func lineTo(p Point) Op      { return Op{"lineTo", []float64{p[0], p[1]}} }
func cubic(a, b, c Point) Op { return Op{"bcurveTo", []float64{a[0], a[1], b[0], b[1], c[0], c[1]}} }
func pathSet(ops []Op) Set   { return Set{"path", ops} }
func offset(min, max float64, o Options, r *random, gain float64) float64 {
	return o.Roughness * gain * (r.next()*(max-min) + min)
}
func off(x float64, o Options, r *random, gain float64) float64 { return offset(-x, x, o, r, gain) }
func drawable(sets []Set, o Options) Drawable                   { return Drawable{sets, o} }
func hasFill(o Options) bool                                    { return o.Fill != "" }
func Line(x1, y1, x2, y2 float64, o Options) Drawable {
	return drawable([]Set{pathSet(doubleLine(x1, y1, x2, y2, o, newRandom(o.Seed), false))}, o)
}
func Rectangle(x, y, w, h float64, o Options) Drawable {
	return polygonShape([]Point{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}, o)
}
func Polygon(ps []Point, o Options) Drawable { return polygonShape(ps, o) }
func polygonShape(ps []Point, o Options) Drawable {
	r := newRandom(o.Seed)
	outline := linearOps(ps, true, o, r)
	var sets []Set
	if hasFill(o) {
		if o.FillStyle == "solid" {
			sets = append(sets, solidFill([][]Point{ps}, o, r))
		} else {
			sets = append(sets, patternFill([][]Point{append([]Point{}, ps...)}, o, r))
		}
	}
	if o.Stroke != "none" {
		sets = append(sets, pathSet(outline))
	}
	return drawable(sets, o)
}
func LinearPath(ps []Point, o Options) Drawable {
	return drawable([]Set{pathSet(linearOps(ps, false, o, newRandom(o.Seed)))}, o)
}
func linearOps(ps []Point, close bool, o Options, r *random) []Op {
	var ops []Op
	for i := 1; i < len(ps); i++ {
		ops = append(ops, doubleLine(ps[i-1][0], ps[i-1][1], ps[i][0], ps[i][1], o, r, false)...)
	}
	if close && len(ps) > 2 {
		a, b := ps[len(ps)-1], ps[0]
		ops = append(ops, doubleLine(a[0], a[1], b[0], b[1], o, r, false)...)
	}
	return ops
}
func doubleLine(x1, y1, x2, y2 float64, o Options, r *random, fill bool) []Op {
	ops := lineOps(x1, y1, x2, y2, o, r, true, false)
	if (fill && o.DisableMultiStrokeFill) || (!fill && o.DisableMultiStroke) {
		return ops
	}
	return append(ops, lineOps(x1, y1, x2, y2, o, r, true, true)...)
}
func lineOps(x1, y1, x2, y2 float64, o Options, r *random, mv, overlay bool) []Op {
	l2 := (x1-x2)*(x1-x2) + (y1-y2)*(y1-y2)
	l := math.Sqrt(l2)
	gain := float64(1)
	if l > 500 {
		gain = .4
	} else if l >= 200 {
		gain = -.0016668*l + 1.233334
	}
	of := o.MaxRandomnessOffset
	if of*of*100 > l2 {
		of = l / 10
	}
	half := of / 2
	div := .2 + r.next()*.2
	mx := o.Bowing * o.MaxRandomnessOffset * (y2 - y1) / 200
	my := o.Bowing * o.MaxRandomnessOffset * (x1 - x2) / 200
	mx = off(mx, o, r, gain)
	my = off(my, o, r, gain)
	jitter := func() float64 {
		if overlay {
			return off(half, o, r, gain)
		}
		return off(of, o, r, gain)
	}
	vertex := func() float64 {
		if o.PreserveVertices {
			return 0
		}
		return jitter()
	}
	var ops []Op
	if mv {
		x := x1 + vertex()
		y := y1 + vertex()
		ops = append(ops, move(Point{x, y}))
	}
	a := mx + x1 + (x2-x1)*div + jitter()
	b := my + y1 + (y2-y1)*div + jitter()
	c := mx + x1 + 2*(x2-x1)*div + jitter()
	d := my + y1 + 2*(y2-y1)*div + jitter()
	e := x2 + vertex()
	f := y2 + vertex()
	return append(ops, cubic(Point{a, b}, Point{c, d}, Point{e, f}))
}
func solidFill(polys [][]Point, o Options, r *random) Set {
	var ops []Op
	for _, ps := range polys {
		if len(ps) < 3 {
			continue
		}
		x := ps[0][0] + off(o.MaxRandomnessOffset, o, r, 1)
		ops = append(ops, move(Point{x, ps[0][1] + off(o.MaxRandomnessOffset, o, r, 1)}))
		for _, p := range ps[1:] {
			x := p[0] + off(o.MaxRandomnessOffset, o, r, 1)
			ops = append(ops, lineTo(Point{x, p[1] + off(o.MaxRandomnessOffset, o, r, 1)}))
		}
	}
	return Set{"fillPath", ops}
}

func Curve(ps []Point, o Options) Drawable {
	r := newRandom(o.Seed)
	outline := curveSet(ps, o, r)
	var sets []Set
	if hasFill(o) && o.Fill != "none" && len(ps) >= 3 {
		if o.FillStyle == "solid" {
			sets = append(sets, Set{"fillPath", merged(curveSet(ps, solidOptions(o), r))})
		} else {
			poly := pointsOnBezier(curveToBezier(ps), 10, (1+o.Roughness)/2)
			sets = append(sets, patternFill([][]Point{poly}, o, r))
		}
	}
	if o.Stroke != "none" {
		sets = append(sets, pathSet(outline))
	}
	return drawable(sets, o)
}
func solidOptions(o Options) Options {
	o.DisableMultiStroke = true
	if o.Roughness != 0 {
		o.Roughness += o.FillShapeRoughnessGain
	}
	return o
}
func merged(ops []Op) []Op {
	var out []Op
	for i, p := range ops {
		if i == 0 || p.Op != "move" {
			out = append(out, p)
		}
	}
	return out
}
func curveSet(ps []Point, o Options, r *random) []Op {
	if len(ps) == 0 {
		return nil
	}
	ops := curveOffset(ps, 1*(1+o.Roughness*.2), o, r)
	if !o.DisableMultiStroke {
		p := o
		if p.Seed != 0 {
			p.Seed++
		}
		ops = append(ops, curveOffset(ps, 1.5*(1+o.Roughness*.22), p, newRandom(p.Seed))...)
	}
	return ops
}
func curveOffset(ps []Point, of float64, o Options, r *random) []Op {
	var points []Point
	jit := func(p Point) Point { x := p[0] + off(of, o, r, 1); return Point{x, p[1] + off(of, o, r, 1)} }
	points = append(points, jit(ps[0]), jit(ps[0]))
	for i := 1; i < len(ps); i++ {
		points = append(points, jit(ps[i]))
		if i == len(ps)-1 {
			points = append(points, jit(ps[i]))
		}
	}
	return curveOps(points, o, r)
}
func curveOps(ps []Point, o Options, r *random) []Op {
	var ops []Op
	n := len(ps)
	if n > 3 {
		s := 1 - o.CurveTightness
		ops = append(ops, move(ps[1]))
		for i := 1; i+2 < n; i++ {
			c := ps[i]
			a := Point{c[0] + (s*ps[i+1][0]-s*ps[i-1][0])/6, c[1] + (s*ps[i+1][1]-s*ps[i-1][1])/6}
			b := Point{ps[i+1][0] + (s*ps[i][0]-s*ps[i+2][0])/6, ps[i+1][1] + (s*ps[i][1]-s*ps[i+2][1])/6}
			ops = append(ops, cubic(a, b, ps[i+1]))
		}
	} else if n == 3 {
		ops = append(ops, move(ps[1]), cubic(ps[1], ps[2], ps[2]))
	} else if n == 2 {
		ops = doubleLine(ps[0][0], ps[0][1], ps[1][0], ps[1][1], o, r, false)
	}
	return ops
}

type ellipseParams struct{ increment, rx, ry float64 }

func ellipseParameters(w, h float64, o Options, r *random) ellipseParams {
	psq := math.Sqrt(math.Pi * 2 * math.Sqrt(((w/2)*(w/2)+(h/2)*(h/2))/2))
	steps := math.Ceil(math.Max(o.CurveStepCount, o.CurveStepCount/math.Sqrt(200)*psq))
	rx, ry := math.Abs(w/2), math.Abs(h/2)
	rx += off(rx*(1-o.CurveFitting), o, r, 1)
	ry += off(ry*(1-o.CurveFitting), o, r, 1)
	return ellipseParams{math.Pi * 2 / steps, rx, ry}
}
func Ellipse(x, y, w, h float64, o Options) Drawable {
	r := newRandom(o.Seed)
	p := ellipseParameters(w, h, o, r)
	estimated, outline := ellipseOps(x, y, o, p, r)
	var sets []Set
	if hasFill(o) {
		if o.FillStyle == "solid" {
			_, shape := ellipseOps(x, y, o, p, r)
			sets = append(sets, Set{"fillPath", shape})
		} else {
			sets = append(sets, patternFill([][]Point{estimated}, o, r))
		}
	}
	if o.Stroke != "none" {
		sets = append(sets, pathSet(outline))
	}
	return drawable(sets, o)
}
func ellipseOps(x, y float64, o Options, p ellipseParams, r *random) ([]Point, []Op) {
	inner := offset(.4, 1, o, r, 1)
	overlap := p.increment * offset(.1, inner, o, r, 1)
	ap, cp := ellipsePoints(p.increment, x, y, p.rx, p.ry, 1, overlap, o, r)
	ops := curveOps(ap, o, r)
	if !o.DisableMultiStroke && o.Roughness != 0 {
		ap, _ = ellipsePoints(p.increment, x, y, p.rx, p.ry, 1.5, 0, o, r)
		ops = append(ops, curveOps(ap, o, r)...)
	}
	return cp, ops
}
func ellipsePoints(increment, cx, cy, rx, ry, of, overlap float64, o Options, r *random) ([]Point, []Point) {
	var all, core []Point
	if o.Roughness == 0 {
		inc := increment / 4
		all = append(all, Point{cx + rx*math.Cos(-inc), cy + ry*math.Sin(-inc)})
		for a := float64(0); a <= math.Pi*2; a += inc {
			p := Point{cx + rx*math.Cos(a), cy + ry*math.Sin(a)}
			core = append(core, p)
			all = append(all, p)
		}
		all = append(all, Point{cx + rx, cy}, Point{cx + rx*math.Cos(inc), cy + ry*math.Sin(inc)})
	} else {
		rad := off(.5, o, r, 1) - math.Pi/2
		pt := func(k, a float64) Point {
			x := off(of, o, r, 1) + cx + k*rx*math.Cos(a)
			return Point{x, off(of, o, r, 1) + cy + k*ry*math.Sin(a)}
		}
		all = append(all, pt(.9, rad-increment))
		for a := rad; a < math.Pi*2+rad-.01; a += increment {
			p := pt(1, a)
			core = append(core, p)
			all = append(all, p)
		}
		all = append(all, pt(1, rad+math.Pi*2+overlap*.5), pt(.98, rad+overlap), pt(.9, rad+overlap*.5))
	}
	return all, core
}
func Arc(x, y, w, h, start, stop float64, closed bool, o Options) Drawable {
	r := newRandom(o.Seed)
	outline := arcOps(x, y, w, h, start, stop, closed, true, o, r)
	var sets []Set
	if closed && hasFill(o) {
		if o.FillStyle == "solid" {
			fo := o
			fo.DisableMultiStroke = true
			sets = append(sets, Set{"fillPath", arcOps(x, y, w, h, start, stop, true, false, fo, r)})
		} else {
			rx, ry := math.Abs(w/2), math.Abs(h/2)
			rx += off(rx*.01, o, r, 1)
			ry += off(ry*.01, o, r, 1)
			start, stop = arcRange(start, stop)
			inc := (stop - start) / o.CurveStepCount
			var ps []Point
			if inc > 0 {
				for a := start; a <= stop; a += inc {
					ps = append(ps, Point{x + rx*math.Cos(a), y + ry*math.Sin(a)})
				}
			}
			ps = append(ps, Point{x + rx*math.Cos(stop), y + ry*math.Sin(stop)}, Point{x, y})
			sets = append(sets, patternFill([][]Point{ps}, o, r))
		}
	}
	if o.Stroke != "none" {
		sets = append(sets, pathSet(outline))
	}
	return drawable(sets, o)
}
func arcRange(start, stop float64) (float64, float64) {
	for start < 0 {
		start += math.Pi * 2
		stop += math.Pi * 2
	}
	if stop-start > math.Pi*2 {
		return 0, math.Pi * 2
	}
	return start, stop
}
func arcOps(cx, cy, w, h, start, stop float64, closed, roughClosure bool, o Options, r *random) []Op {
	rx, ry := math.Abs(w/2), math.Abs(h/2)
	rx += off(rx*.01, o, r, 1)
	ry += off(ry*.01, o, r, 1)
	start, stop = arcRange(start, stop)
	inc := math.Min(math.Pi/o.CurveStepCount, (stop-start)/2)
	ops := arcCurve(inc, cx, cy, rx, ry, start, stop, 1, o, r)
	if !o.DisableMultiStroke {
		ops = append(ops, arcCurve(inc, cx, cy, rx, ry, start, stop, 1.5, o, r)...)
	}
	if closed {
		a, b := Point{cx + rx*math.Cos(start), cy + ry*math.Sin(start)}, Point{cx + rx*math.Cos(stop), cy + ry*math.Sin(stop)}
		if roughClosure {
			ops = append(ops, doubleLine(cx, cy, a[0], a[1], o, r, false)...)
			ops = append(ops, doubleLine(cx, cy, b[0], b[1], o, r, false)...)
		} else {
			ops = append(ops, lineTo(Point{cx, cy}), lineTo(a))
		}
	}
	return ops
}
func arcCurve(inc, cx, cy, rx, ry, start, stop, of float64, o Options, r *random) []Op {
	if inc <= 0 {
		return nil
	}
	rad := start + off(.1, o, r, 1)
	pt := func(k, a float64) Point {
		x := off(of, o, r, 1) + cx + k*rx*math.Cos(a)
		return Point{x, off(of, o, r, 1) + cy + k*ry*math.Sin(a)}
	}
	ps := []Point{pt(.9, rad-inc)}
	for a := rad; a <= stop; a += inc {
		ps = append(ps, pt(1, a))
	}
	ps = append(ps, Point{cx + rx*math.Cos(stop), cy + ry*math.Sin(stop)}, Point{cx + rx*math.Cos(stop), cy + ry*math.Sin(stop)})
	return curveOps(ps, o, r)
}
