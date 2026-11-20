// SVG parsing and arc normalization follow path-data-parser 0.1.0 and
// points-on-path 0.2.1 (MIT, Preet Shihn), via Quiet.md board/rough/path_data.rs.
package rough

import (
	"math"
	"strconv"
	"strings"
)

type Segment struct {
	Key  byte
	Data []float64
}

func params(c byte) int {
	switch c {
	case 'A', 'a':
		return 7
	case 'C', 'c':
		return 6
	case 'H', 'h', 'V', 'v':
		return 1
	case 'L', 'l', 'M', 'm', 'T', 't':
		return 2
	case 'Q', 'q', 'S', 's':
		return 4
	case 'Z', 'z':
		return 0
	}
	return -1
}
func ParsePath(d string) []Segment {
	type token struct {
		cmd byte
		n   float64
	}
	var ts []token
	for i := 0; i < len(d); {
		c := d[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ',' {
			i++
			continue
		}
		if params(c) >= 0 {
			ts = append(ts, token{cmd: c})
			i++
			continue
		}
		start := i
		if c == '-' || c == '+' {
			i++
		}
		digits := 0
		for i < len(d) && d[i] >= '0' && d[i] <= '9' {
			i++
			digits++
		}
		if i < len(d) && d[i] == '.' {
			i++
			for i < len(d) && d[i] >= '0' && d[i] <= '9' {
				i++
				digits++
			}
		}
		if digits == 0 {
			return nil
		}
		if i < len(d) && (d[i] == 'e' || d[i] == 'E') {
			i++
			if i < len(d) && (d[i] == '-' || d[i] == '+') {
				i++
			}
			for i < len(d) && d[i] >= '0' && d[i] <= '9' {
				i++
			}
		}
		v, err := strconv.ParseFloat(d[start:i], 64)
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil
		}
		ts = append(ts, token{n: v})
	}
	if len(ts) > 0 && ts[0].cmd != 'M' && ts[0].cmd != 'm' {
		return ParsePath("M0,0" + d)
	}
	var out []Segment
	mode := byte('M')
	for i := 0; i < len(ts); {
		if ts[i].cmd != 0 {
			mode = ts[i].cmd
			i++
		}
		n := params(mode)
		if n < 0 || i+n > len(ts) {
			return nil
		}
		values := make([]float64, n)
		for j := 0; j < n; j++ {
			if ts[i+j].cmd != 0 {
				return nil
			}
			values[j] = ts[i+j].n
		}
		out = append(out, Segment{mode, values})
		i += n
		if mode == 'M' {
			mode = 'L'
		} else if mode == 'm' {
			mode = 'l'
		}
		if n == 0 && i < len(ts) && ts[i].cmd == 0 {
			return nil
		}
	}
	return out
}
func absolutize(ss []Segment) []Segment {
	var out []Segment
	cx, cy, sx, sy := float64(0), float64(0), float64(0), float64(0)
	for _, s := range ss {
		k := s.Key
		d := append([]float64{}, s.Data...)
		rel := k >= 'a' && k <= 'z'
		if rel {
			k -= 32
		}
		switch k {
		case 'M', 'L', 'C', 'Q', 'S', 'T':
			if rel {
				for i := range d {
					if i%2 == 0 {
						d[i] += cx
					} else {
						d[i] += cy
					}
				}
			}
			cx, cy = d[len(d)-2], d[len(d)-1]
			if k == 'M' {
				sx, sy = cx, cy
			}
		case 'A':
			if rel {
				d[5] += cx
				d[6] += cy
			}
			cx, cy = d[5], d[6]
		case 'H':
			if rel {
				d[0] += cx
			}
			cx = d[0]
		case 'V':
			if rel {
				d[0] += cy
			}
			cy = d[0]
		case 'Z':
			cx, cy = sx, sy
		}
		out = append(out, Segment{k, d})
	}
	return out
}
func NormalizePath(ss []Segment) []Segment {
	var out []Segment
	last := byte(' ')
	cx, cy, sx, sy, lx, ly := float64(0), float64(0), float64(0), float64(0), float64(0), float64(0)
	for _, s := range absolutize(ss) {
		d := s.Data
		switch s.Key {
		case 'M':
			out = append(out, s)
			cx, cy = d[0], d[1]
			sx, sy = cx, cy
		case 'C':
			out = append(out, s)
			cx, cy = d[4], d[5]
			lx, ly = d[2], d[3]
		case 'L':
			out = append(out, s)
			cx, cy = d[0], d[1]
		case 'H':
			cx = d[0]
			out = append(out, Segment{'L', []float64{cx, cy}})
		case 'V':
			cy = d[0]
			out = append(out, Segment{'L', []float64{cx, cy}})
		case 'S':
			x1, y1 := cx, cy
			if last == 'C' || last == 'S' {
				x1 = 2*cx - lx
				y1 = 2*cy - ly
			}
			out = append(out, Segment{'C', []float64{x1, y1, d[0], d[1], d[2], d[3]}})
			lx, ly = d[0], d[1]
			cx, cy = d[2], d[3]
		case 'T', 'Q':
			x1, y1, x, y := cx, cy, d[0], d[1]
			if s.Key == 'Q' {
				x1, y1, x, y = d[0], d[1], d[2], d[3]
			} else if last == 'Q' || last == 'T' {
				x1 = 2*cx - lx
				y1 = 2*cy - ly
			}
			out = append(out, Segment{'C', []float64{cx + 2*(x1-cx)/3, cy + 2*(y1-cy)/3, x + 2*(x1-x)/3, y + 2*(y1-y)/3, x, y}})
			lx, ly = x1, y1
			cx, cy = x, y
		case 'A':
			r1, r2, x, y := math.Abs(d[0]), math.Abs(d[1]), d[5], d[6]
			if r1 == 0 || r2 == 0 {
				out = append(out, Segment{'C', []float64{cx, cy, x, y, x, y}})
			} else if cx != x || cy != y {
				for _, c := range arcToCubic(cx, cy, x, y, r1, r2, d[2], d[3], d[4], nil) {
					out = append(out, Segment{'C', c[:]})
				}
			}
			cx, cy = x, y
		case 'Z':
			out = append(out, s)
			cx, cy = sx, sy
		}
		last = s.Key
	}
	return out
}
func rotate(x, y, a float64) (float64, float64) {
	return x*math.Cos(a) - y*math.Sin(a), x*math.Sin(a) + y*math.Cos(a)
}
func fixed9(v float64) float64 {
	s := strconv.FormatFloat(v, 'f', 9, 64)
	n, _ := strconv.ParseFloat(s, 64)
	return n
}
func arcToCubic(x1, y1, x2, y2, r1, r2, angle, large, sweep float64, recursive *[4]float64) [][6]float64 {
	ar := math.Pi * angle / 180
	sw := sweep != 0
	var f1, f2, cx, cy float64
	var rest []Point
	if recursive != nil {
		f1, f2, cx, cy = recursive[0], recursive[1], recursive[2], recursive[3]
	} else {
		x1, y1 = rotate(x1, y1, -ar)
		x2, y2 = rotate(x2, y2, -ar)
		x, y := (x1-x2)/2, (y1-y2)/2
		h := x*x/(r1*r1) + y*y/(r2*r2)
		if h > 1 {
			h = math.Sqrt(h)
			r1 *= h
			r2 *= h
		}
		sign := float64(1)
		if (large != 0) == sw {
			sign = -1
		}
		r1p, r2p := r1*r1, r2*r2
		k := sign * math.Sqrt(math.Abs((r1p*r2p-r1p*y*y-r2p*x*x)/(r1p*y*y+r2p*x*x)))
		cx = k*r1*y/r2 + (x1+x2)/2
		cy = k*(-r2)*x/r1 + (y1+y2)/2
		f1 = math.Asin(fixed9((y1 - cy) / r2))
		f2 = math.Asin(fixed9((y2 - cy) / r2))
		if x1 < cx {
			f1 = math.Pi - f1
		}
		if x2 < cx {
			f2 = math.Pi - f2
		}
		if f1 < 0 {
			f1 += math.Pi * 2
		}
		if f2 < 0 {
			f2 += math.Pi * 2
		}
		if sw && f1 > f2 {
			f1 -= math.Pi * 2
		}
		if !sw && f2 > f1 {
			f2 -= math.Pi * 2
		}
	}
	df := f2 - f1
	mx := math.Pi * 120 / 180
	if math.Abs(df) > mx {
		f2old, x2old, y2old := f2, x2, y2
		if sw && f2 > f1 {
			f2 = f1 + mx
		} else {
			f2 = f1 - mx
		}
		x2 = cx + r1*math.Cos(f2)
		y2 = cy + r2*math.Sin(f2)
		for _, c := range arcToCubic(x2, y2, x2old, y2old, r1, r2, angle, 0, sweep, &[4]float64{f2, f2old, cx, cy}) {
			rest = append(rest, Point{c[0], c[1]}, Point{c[2], c[3]}, Point{c[4], c[5]})
		}
	}
	df = f2 - f1
	t := math.Tan(df / 4)
	hx, hy := 4.0/3*r1*t, 4.0/3*r2*t
	m2 := Point{x1 + hx*math.Sin(f1), y1 - hy*math.Cos(f1)}
	m2[0] = 2*x1 - m2[0]
	m2[1] = 2*y1 - m2[1]
	all := []Point{m2, {x2 + hx*math.Sin(f2), y2 - hy*math.Cos(f2)}, {x2, y2}}
	all = append(all, rest...)
	var out [][6]float64
	for i := 0; i < len(all); i += 3 {
		a, b, c := all[i], all[i+1], all[i+2]
		if recursive == nil {
			a[0], a[1] = rotate(a[0], a[1], ar)
			b[0], b[1] = rotate(b[0], b[1], ar)
			c[0], c[1] = rotate(c[0], c[1], ar)
		}
		out = append(out, [6]float64{a[0], a[1], b[0], b[1], c[0], c[1]})
	}
	return out
}

func pointsOnPath(ss []Segment, tolerance, distance float64) [][]Point {
	var sets [][]Point
	var cur, pending []Point
	start := Point{}
	flush := func() {
		if len(pending) >= 4 {
			cur = append(cur, pointsOnBezier(pending, tolerance, 0)...)
		}
		pending = nil
	}
	for _, s := range ss {
		d := s.Data
		switch s.Key {
		case 'M':
			flush()
			if len(cur) > 0 {
				sets = append(sets, cur)
				cur = nil
			}
			start = Point{d[0], d[1]}
			cur = append(cur, start)
		case 'L':
			flush()
			cur = append(cur, Point{d[0], d[1]})
		case 'C':
			if len(pending) == 0 {
				p := start
				if len(cur) > 0 {
					p = cur[len(cur)-1]
				}
				pending = append(pending, p)
			}
			pending = append(pending, Point{d[0], d[1]}, Point{d[2], d[3]}, Point{d[4], d[5]})
		case 'Z':
			flush()
			cur = append(cur, start)
		}
	}
	flush()
	if len(cur) > 0 {
		sets = append(sets, cur)
	}
	if distance != 0 {
		for i, s := range sets {
			var out []Point
			simplifyPoints(s, distance, &out)
			sets[i] = out
		}
	}
	return sets
}
func bezierTo(d []float64, cur Point, o Options, r *random) []Op {
	var ops []Op
	m := o.MaxRandomnessOffset
	if m == 0 {
		m = 1
	}
	ros := []float64{m, m + .3}
	if o.DisableMultiStroke {
		ros = ros[:1]
	}
	for i, ro := range ros {
		if i == 0 {
			ops = append(ops, move(cur))
		} else {
			mx, my := cur[0], cur[1]
			if !o.PreserveVertices {
				mx += off(ros[0], o, r, 1)
				my += off(ros[0], o, r, 1)
			}
			ops = append(ops, move(Point{mx, my}))
		}
		f := Point{d[4], d[5]}
		if !o.PreserveVertices {
			f[0] += off(ro, o, r, 1)
			f[1] += off(ro, o, r, 1)
		}
		a := d[0] + off(ro, o, r, 1)
		b := d[1] + off(ro, o, r, 1)
		c := d[2] + off(ro, o, r, 1)
		dd := d[3] + off(ro, o, r, 1)
		ops = append(ops, cubic(Point{a, b}, Point{c, dd}, f))
	}
	return ops
}
func svgOps(ss []Segment, o Options, r *random) []Op {
	var ops []Op
	first, cur := Point{}, Point{}
	for _, s := range ss {
		d := s.Data
		switch s.Key {
		case 'M':
			cur = Point{d[0], d[1]}
			first = cur
		case 'L':
			ops = append(ops, doubleLine(cur[0], cur[1], d[0], d[1], o, r, false)...)
			cur = Point{d[0], d[1]}
		case 'C':
			ops = append(ops, bezierTo(d, cur, o, r)...)
			cur = Point{d[4], d[5]}
		case 'Z':
			ops = append(ops, doubleLine(cur[0], cur[1], first[0], first[1], o, r, false)...)
			cur = first
		}
	}
	return ops
}
func Path(data string, o Options) Drawable {
	data = strings.ReplaceAll(strings.ReplaceAll(data, "\n", " "), "- ", "-")
	ss := NormalizePath(ParsePath(data))
	if len(ss) == 0 {
		return drawable(nil, o)
	}
	r := newRandom(o.Seed)
	fill := hasFill(o) && o.Fill != "transparent" && o.Fill != "none"
	simplified := o.Simplification != nil && *o.Simplification != 0 && *o.Simplification < 1
	distance := (1 + o.Roughness) / 2
	if simplified {
		distance = 4 - 4*(*o.Simplification)
	}
	sets := pointsOnPath(ss, 1, distance)
	shape := svgOps(ss, o, r)
	var out []Set
	if fill {
		if o.FillStyle == "solid" {
			if len(sets) == 1 {
				out = append(out, Set{"fillPath", merged(svgOps(ss, solidOptions(o), r))})
			} else {
				out = append(out, solidFill(sets, o, r))
			}
		} else {
			out = append(out, patternFill(sets, o, r))
		}
	}
	if o.Stroke != "none" {
		if simplified {
			for _, s := range sets {
				out = append(out, pathSet(linearOps(s, false, o, r)))
			}
		} else {
			out = append(out, pathSet(shape))
		}
	}
	return drawable(out, o)
}
