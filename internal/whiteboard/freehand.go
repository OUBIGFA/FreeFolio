// Pressure-sensitive outlines port perfect-freehand 1.2.0 (MIT, Steve Ruiz)
// through Quiet.md e59c49a's board/freehand.rs. Tapers are off and caps are on,
// matching the options used by Excalidraw.
package whiteboard

import "math"

type strokeInput struct {
	p           Point
	pressure    float64
	hasPressure bool
}
type strokePoint struct {
	p, v                        Point
	pressure, distance, running float64
}
type freehandOptions struct {
	size, thinning, smoothing, streamline float64
	simulate                              bool
	easing                                func(float64) float64
	taper                                 float64
}

func perp(p Point) Point { return Point{p[1], -p[0]} }
func unit(p Point) Point {
	l := math.Hypot(p[0], p[1])
	if l == 0 {
		return Point{}
	}
	return ScalePoint(p, 1/l)
}
func dot(a, b Point) float64        { return a[0]*b[0] + a[1]*b[1] }
func distanceSq(a, b Point) float64 { d := Sub(a, b); return dot(d, d) }

func FreehandOutline(e *Element) []Point {
	o := freehandOptions{size: e.StrokeWidth * 4.25, thinning: .6, smoothing: .5, streamline: .5, simulate: e.SimulatePressure, easing: func(t float64) float64 { return math.Sin(t * math.Pi / 2) }}
	constant := e.StrokeOptions != nil && e.StrokeOptions.Variability == "constant"
	if e.StrokeOptions != nil {
		o.streamline = e.StrokeOptions.Streamline
	}
	if constant {
		o.size = e.StrokeWidth * 1.4 * 2
		o.thinning = 0
		o.simulate = false
		o.easing = func(t float64) float64 { return t }
	}
	var in []strokeInput
	for i, p := range e.Points {
		v := strokeInput{p: p}
		if constant {
			v.pressure = 1
			v.hasPressure = true
		} else if !o.simulate && i < len(e.Pressures) {
			v.pressure = e.Pressures[i]
			v.hasPressure = true
		}
		in = append(in, v)
	}
	if len(in) == 0 && !constant {
		in = []strokeInput{{pressure: .5, hasPressure: true}}
	}
	return strokeOutline(strokePoints(in, o), o)
}
func strokePoints(in []strokeInput, o freehandOptions) []strokePoint {
	if len(in) == 0 {
		return nil
	}
	t := .15 + (1-o.streamline)*.85
	pts := append([]strokeInput{}, in...)
	if len(pts) == 2 {
		last := pts[1]
		pts = pts[:1]
		for i := 1; i < 5; i++ {
			pts = append(pts, strokeInput{p: Lerp(pts[0].p, last.p, float64(i)/4)})
		}
	}
	if len(pts) == 1 {
		p := pts[0]
		p.p = Add(p.p, Point{1, 1})
		pts = append(pts, p)
	}
	pressure := func(p strokeInput, d float64) float64 {
		if p.hasPressure && p.pressure >= 0 {
			return p.pressure
		}
		return d
	}
	out := []strokePoint{{p: pts[0].p, pressure: pressure(pts[0], .25), v: Point{1, 1}}}
	reached := false
	running := float64(0)
	prev := out[0]
	last := len(pts) - 1
	for i, p := range pts[1:] {
		at := i + 1
		point := Lerp(prev.p, p.p, t)
		if at == last {
			point = p.p
		}
		if point == prev.p {
			continue
		}
		dist := Distance(point, prev.p)
		running += dist
		if at < last && !reached {
			if running < o.size {
				continue
			}
			reached = true
		}
		prev = strokePoint{p: point, pressure: pressure(p, .5), v: unit(Sub(prev.p, point)), distance: dist, running: running}
		out = append(out, prev)
	}
	out[0].v = Point{}
	if len(out) > 1 {
		out[0].v = out[1].v
	}
	return out
}
func strokeOutline(ps []strokePoint, o freehandOptions) []Point {
	size := o.size
	if len(ps) == 0 || size <= 0 {
		return nil
	}
	n := len(ps)
	total := ps[n-1].running
	minDistance := (size * o.smoothing) * (size * o.smoothing)
	var left, right []Point
	simulated := func(prev float64, p strokePoint) float64 {
		sp := math.Min(p.distance/size, 1)
		rp := math.Min(1-sp, 1)
		return math.Min(prev+(rp-prev)*(sp*.275), 1)
	}
	prevPressure := ps[0].pressure
	for _, p := range ps[:min(10, n)] {
		pressure := p.pressure
		if o.simulate {
			pressure = simulated(prevPressure, p)
		}
		prevPressure = (prevPressure + pressure) / 2
	}
	radiusOf := func(p float64) float64 { return size * o.easing(.5-o.thinning*(.5-p)) }
	radius := radiusOf(ps[n-1].pressure)
	firstRadius := float64(-1)
	prevVector := ps[0].v
	pl, pr := ps[0].p, ps[0].p
	prevSharp := false
	fixedPi := math.Pi + .0001
	for i, p := range ps {
		if i < n-1 && total-p.running < 3 {
			continue
		}
		pressure := p.pressure
		if o.thinning != 0 {
			if o.simulate {
				pressure = simulated(prevPressure, p)
			}
			radius = radiusOf(pressure)
		} else {
			radius = size / 2
		}
		if firstRadius < 0 {
			firstRadius = radius
		}
		startStrength, endStrength := float64(1), float64(1)
		if o.taper > 0 {
			if p.running < o.taper {
				t := p.running / o.taper
				startStrength = t * (2 - t)
			}
			if total-p.running < o.taper {
				t := (total-p.running)/o.taper - 1
				endStrength = t*t*t + 1
			}
		}
		radius = math.Max(radius*math.Min(startStrength, endStrength), .01)
		nextVector := p.v
		nextDot := float64(1)
		if i < n-1 {
			nextVector = ps[i+1].v
			nextDot = dot(p.v, nextVector)
		}
		prevDot := dot(p.v, prevVector)
		sharp, nextSharp := prevDot < 0 && !prevSharp, nextDot < 0
		if sharp || nextSharp {
			of := ScalePoint(perp(prevVector), radius)
			for t := float64(0); t <= 1; t += 1.0 / 13 {
				tl := RotatePoint(Sub(p.p, of), p.p, fixedPi*t)
				tr := RotatePoint(Add(p.p, of), p.p, -fixedPi*t)
				left = append(left, tl)
				right = append(right, tr)
				pl, pr = tl, tr
			}
			if nextSharp {
				prevSharp = true
			}
			continue
		}
		prevSharp = false
		if i == n-1 {
			of := ScalePoint(perp(p.v), radius)
			left = append(left, Sub(p.p, of))
			right = append(right, Add(p.p, of))
			continue
		}
		of := ScalePoint(perp(Lerp(nextVector, p.v, nextDot)), radius)
		tl, tr := Sub(p.p, of), Add(p.p, of)
		if i <= 1 || distanceSq(pl, tl) > minDistance {
			left = append(left, tl)
			pl = tl
		}
		if i <= 1 || distanceSq(pr, tr) > minDistance {
			right = append(right, tr)
			pr = tr
		}
		prevPressure = pressure
		prevVector = p.v
	}
	first, last := ps[0].p, ps[n-1].p
	if n == 1 {
		last = Add(first, Point{1, 1})
		if firstRadius < 0 {
			firstRadius = radius
		}
		start := Add(first, ScalePoint(unit(perp(Sub(first, last))), -firstRadius))
		var circle []Point
		for t := 1.0 / 13; t <= 1; t += 1.0 / 13 {
			circle = append(circle, RotatePoint(start, first, fixedPi*2*t))
		}
		return circle
	}
	var startCap, endCap []Point
	if len(right) > 0 && o.taper == 0 {
		for t := 1.0 / 13; t <= 1; t += 1.0 / 13 {
			startCap = append(startCap, RotatePoint(right[0], first, fixedPi*t))
		}
	}
	if o.taper > 0 {
		endCap = append(endCap, last)
	} else {
		direction := perp(ScalePoint(ps[n-1].v, -1))
		start := Add(last, ScalePoint(direction, radius))
		for t := 1.0 / 29; t < 1; t += 1.0 / 29 {
			endCap = append(endCap, RotatePoint(start, last, fixedPi*3*t))
		}
	}
	out := append(left, endCap...)
	for i := len(right) - 1; i >= 0; i-- {
		out = append(out, right[i])
	}
	return append(out, startCap...)
}
