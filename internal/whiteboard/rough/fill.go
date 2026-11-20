// Scanline fillers port hachure-fill 0.5.2 and rough.js fillers through
// Quiet.md e59c49a. Both are MIT, Copyright Preet Shihn.
package rough

import (
	"math"
	"sort"
)

type segment [2]Point

func gapOf(o Options) float64 {
	g := o.HachureGap
	if g < 0 {
		g = o.StrokeWidth * 4
	}
	return math.Max(.1, g)
}
func renderLines(ls []segment, o Options, r *random) []Op {
	var ops []Op
	for _, l := range ls {
		ops = append(ops, doubleLine(l[0][0], l[0][1], l[1][0], l[1][1], o, r, true)...)
	}
	return ops
}
func hachureOps(ps [][]Point, o Options, r *random) []Op {
	return renderLines(polygonHachure(ps, o, r), o, r)
}
func polygonHachure(ps [][]Point, o Options, r *random) []segment {
	skip := float64(1)
	if o.Roughness >= 1 {
		v := float64(0)
		if r.started {
			v = r.next()
		}
		if v == 0 {
			v = r.mathRandom()
		}
		if v > .7 {
			skip = gapOf(o)
		}
	}
	return hachureLines(ps, gapOf(o), o.HachureAngle+90, skip)
}
func rotatePoints(ps []Point, deg float64) {
	a := math.Pi / 180 * deg
	si, co := referenceSincos(a)
	for i, p := range ps {
		ps[i] = Point{p[0]*co - p[1]*si, p[0]*si + p[1]*co}
	}
}
func hachureLines(ps [][]Point, gap, angle, step float64) []segment {
	if angle != 0 {
		for _, p := range ps {
			rotatePoints(p, angle)
		}
	}
	ls := straightHachure(ps, math.Max(.1, gap), step)
	if angle != 0 {
		for _, p := range ps {
			rotatePoints(p, -angle)
		}
		for i := range ls {
			rotatePoints(ls[i][:], -angle)
		}
	}
	return ls
}

type edge struct{ ymin, ymax, x, islope float64 }

func straightHachure(ps [][]Point, gap, step float64) []segment {
	var edges []edge
	for _, poly := range ps {
		if len(poly) < 2 {
			continue
		}
		v := append([]Point{}, poly...)
		if v[0] != v[len(v)-1] {
			v = append(v, v[0])
		}
		if len(v) <= 2 {
			continue
		}
		for i := 1; i < len(v); i++ {
			a, b := v[i-1], v[i]
			if a[1] == b[1] {
				continue
			}
			ymin := math.Min(a[1], b[1])
			x := b[0]
			if ymin == a[1] {
				x = a[0]
			}
			edges = append(edges, edge{ymin, math.Max(a[1], b[1]), x, (b[0] - a[0]) / (b[1] - a[1])})
		}
	}
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.ymin != b.ymin {
			return a.ymin < b.ymin
		}
		if a.x != b.x {
			return a.x < b.x
		}
		return a.ymax < b.ymax
	})
	var lines []segment
	if len(edges) == 0 {
		return lines
	}
	var active []edge
	y, iteration := edges[0].ymin, float64(0)
	next := 0
	for len(active) > 0 || next < len(edges) {
		for next < len(edges) && edges[next].ymin <= y {
			active = append(active, edges[next])
			next++
		}
		keep := active[:0]
		for _, e := range active {
			if e.ymax > y {
				keep = append(keep, e)
			}
		}
		active = keep
		sort.SliceStable(active, func(i, j int) bool { return active[i].x < active[j].x })
		if (step != 1 || math.Mod(iteration, gap) == 0) && len(active) > 1 {
			for i := 0; i+1 < len(active); i += 2 {
				lines = append(lines, segment{{math.Floor(active[i].x + .5), y}, {math.Floor(active[i+1].x + .5), y}})
			}
		}
		y += step
		for i := range active {
			active[i].x += step * active[i].islope
		}
		iteration++
	}
	return lines
}
func ordered(l segment) (Point, Point, float64) {
	a, b := l[0], l[1]
	if a[0] > b[0] {
		a, b = b, a
	}
	return a, b, math.Atan((b[1] - a[1]) / (b[0] - a[0]))
}
func length(l segment) float64 {
	dx, dy := l[0][0]-l[1][0], l[0][1]-l[1][1]
	return math.Sqrt(dx*dx + dy*dy)
}
func patternFill(ps [][]Point, o Options, r *random) Set {
	var ops []Op
	switch o.FillStyle {
	case "cross-hatch":
		ops = hachureOps(ps, o, r)
		p := o
		p.HachureAngle += 90
		ops = append(ops, hachureOps(ps, p, r)...)
	case "zigzag":
		g := gapOf(o)
		p := o
		p.HachureGap = g
		ls := polygonHachure(ps, p, r)
		a := math.Pi / 180 * o.HachureAngle
		gx, gy := g*.5*math.Cos(a), g*.5*math.Sin(a)
		var zz []segment
		for _, l := range ls {
			if length(l) == 0 {
				continue
			}
			zz = append(zz, segment{{l[0][0] - gx, l[0][1] + gy}, l[1]}, segment{{l[0][0] + gx, l[0][1] - gy}, l[1]})
		}
		ops = renderLines(zz, o, r)
	case "zigzag-line":
		g := o.HachureGap
		if g < 0 {
			g = o.StrokeWidth * 4
		}
		zo := o.ZigzagOffset
		if zo < 0 {
			zo = g
		}
		p := o
		p.HachureGap = g + zo
		if zo <= 0 {
			break
		}
		for _, l := range polygonHachure(ps, p, r) {
			count := math.Floor(length(l)/(2*zo) + .5)
			a, _, alpha := ordered(l)
			for i := float64(0); i < count; i++ {
				ls, le := i*2*zo, (i+1)*2*zo
				dz := math.Sqrt(2 * zo * zo)
				s := Point{a[0] + ls*math.Cos(alpha), a[1] + ls*math.Sin(alpha)}
				e := Point{a[0] + le*math.Cos(alpha), a[1] + le*math.Sin(alpha)}
				m := Point{s[0] + dz*math.Cos(alpha+math.Pi/4), s[1] + dz*math.Sin(alpha+math.Pi/4)}
				ops = append(ops, doubleLine(s[0], s[1], m[0], m[1], p, r, true)...)
				ops = append(ops, doubleLine(m[0], m[1], e[0], e[1], p, r, true)...)
			}
		}
	case "dots":
		p := o
		p.HachureAngle = 0
		ls := polygonHachure(ps, p, r)
		g := gapOf(p)
		fw := p.FillWeight
		if fw < 0 {
			fw = p.StrokeWidth / 2
		}
		ro := g / 4
		for _, l := range ls {
			ln := length(l)
			count := math.Ceil(ln/g) - 1
			of := ln - count*g
			x := (l[0][0]+l[1][0])/2 - g/4
			ymin := math.Min(l[0][1], l[1][1])
			for i := float64(0); i < count; i++ {
				y := ymin + of + i*g
				cx := (x - ro) + r.mathRandom()*2*ro
				cy := (y - ro) + r.mathRandom()*2*ro
				ep := ellipseParameters(fw, fw, p, r)
				_, es := ellipseOps(cx, cy, p, ep, r)
				ops = append(ops, es...)
			}
		}
	case "dashed":
		ls := polygonHachure(ps, o, r)
		def := o.HachureGap
		if def < 0 {
			def = o.StrokeWidth * 4
		}
		of, g := o.DashOffset, o.DashGap
		if of < 0 {
			of = def
		}
		if g < 0 {
			g = def
		}
		if of+g <= 0 {
			break
		}
		for _, l := range ls {
			ln := length(l)
			count := math.Floor(ln / (of + g))
			so := (ln + g - count*(of+g)) / 2
			a, _, alpha := ordered(l)
			for i := float64(0); i < count; i++ {
				ls, le := i*(of+g), i*(of+g)+of
				s := Point{a[0] + ls*math.Cos(alpha) + so*math.Cos(alpha), a[1] + ls*math.Sin(alpha) + so*math.Sin(alpha)}
				e := Point{a[0] + le*math.Cos(alpha) + so*math.Cos(alpha), a[1] + le*math.Sin(alpha) + so*math.Sin(alpha)}
				ops = append(ops, doubleLine(s[0], s[1], e[0], e[1], o, r, true)...)
			}
		}
	default:
		ops = hachureOps(ps, o, r)
	}
	return Set{"fillSketch", ops}
}
