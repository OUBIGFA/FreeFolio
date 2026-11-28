package whiteboard

import "math"

type Box struct{ X, Y, W, H float64 }

func (b Box) Empty() bool { return b.W <= 0 && b.H <= 0 }
func (b Box) Contains(p Point) bool {
	return p[0] >= b.X && p[0] <= b.X+b.W && p[1] >= b.Y && p[1] <= b.Y+b.H
}
func (b Box) Intersects(o Box) bool {
	return b.X <= o.X+o.W && b.X+b.W >= o.X && b.Y <= o.Y+o.H && b.Y+b.H >= o.Y
}
func (b Box) Center() Point        { return Point{b.X + b.W/2, b.Y + b.H/2} }
func (b Box) Expand(n float64) Box { return Box{b.X - n, b.Y - n, b.W + 2*n, b.H + 2*n} }
func Union(a, b Box) Box {
	x := math.Min(a.X, b.X)
	y := math.Min(a.Y, b.Y)
	return Box{x, y, math.Max(a.X+a.W, b.X+b.W) - x, math.Max(a.Y+a.H, b.Y+b.H) - y}
}
func RotatePoint(p, c Point, angle float64) Point {
	si, co := math.Sincos(angle)
	x, y := p[0]-c[0], p[1]-c[1]
	return Point{c[0] + x*co - y*si, c[1] + x*si + y*co}
}
func Distance(a, b Point) float64         { return math.Hypot(a[0]-b[0], a[1]-b[1]) }
func Add(a, b Point) Point                { return Point{a[0] + b[0], a[1] + b[1]} }
func Sub(a, b Point) Point                { return Point{a[0] - b[0], a[1] - b[1]} }
func ScalePoint(p Point, k float64) Point { return Point{p[0] * k, p[1] * k} }
func Lerp(a, b Point, t float64) Point    { return Point{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t} }
func SegmentDistance(p, a, b Point) float64 {
	v := Sub(b, a)
	l := v[0]*v[0] + v[1]*v[1]
	if l == 0 {
		return Distance(p, a)
	}
	d := Sub(p, a)
	t := math.Max(0, math.Min(1, (d[0]*v[0]+d[1]*v[1])/l))
	return Distance(p, Lerp(a, b, t))
}

// WorldPoints includes rotation around the element's local bounding center.
func WorldPoints(e *Element) []Point {
	var ps []Point
	if e.IsLinear() || e.Kind == KindFreedraw {
		points := e.Points
		if e.DimArrow {
			points = DimArrowPoints(e)
		}
		for _, p := range points {
			ps = append(ps, Point{e.X + p[0], e.Y + p[1]})
		}
	} else if e.Kind == KindDiamond {
		ps = []Point{{e.X + e.W/2, e.Y}, {e.X + e.W, e.Y + e.H/2}, {e.X + e.W/2, e.Y + e.H}, {e.X, e.Y + e.H/2}}
	} else {
		ps = []Point{{e.X, e.Y}, {e.X + e.W, e.Y}, {e.X + e.W, e.Y + e.H}, {e.X, e.Y + e.H}}
	}
	if e.Angle != 0 {
		c := Point{e.X + e.W/2, e.Y + e.H/2}
		for i, p := range ps {
			ps[i] = RotatePoint(p, c, e.Angle)
		}
	}
	return ps
}
func PointsBounds(ps []Point) Box {
	if len(ps) == 0 {
		return Box{}
	}
	x, y, X, Y := ps[0][0], ps[0][1], ps[0][0], ps[0][1]
	for _, p := range ps[1:] {
		x = math.Min(x, p[0])
		y = math.Min(y, p[1])
		X = math.Max(X, p[0])
		Y = math.Max(Y, p[1])
	}
	return Box{x, y, X - x, Y - y}
}
func ElementBounds(e *Element) Box {
	if e.Shape != "" {
		var out Box
		center := Point{e.X + e.W/2, e.Y + e.H/2}
		for i, part := range dimParts(e) {
			box := Box{e.X + part.x, e.Y + part.y, part.w, part.h}
			// The rough fill is offset left/up; layered shapes also extend
			// past their logical box. Include both in culling and exports.
			if e.Roughness > 0 {
				box.X -= 3
				box.Y -= 2
				box.W += 3
				box.H += 2
			}
			ps := []Point{{box.X, box.Y}, {box.X + box.W, box.Y}, {box.X + box.W, box.Y + box.H}, {box.X, box.Y + box.H}}
			for j := range ps {
				if e.Scale[0] < 0 {
					ps[j][0] = 2*e.X + e.W - ps[j][0]
				}
				if e.Scale[1] < 0 {
					ps[j][1] = 2*e.Y + e.H - ps[j][1]
				}
				ps[j] = RotatePoint(ps[j], center, e.Angle)
			}
			bounds := PointsBounds(ps)
			if i == 0 {
				out = bounds
			} else {
				out = Union(out, bounds)
			}
		}
		return out
	}
	if e.Kind == KindFreedraw {
		ps := FreehandOutline(e)
		c := Point{e.X + e.W/2, e.Y + e.H/2}
		for i, p := range ps {
			ps[i] = RotatePoint(Add(p, Point{e.X, e.Y}), c, e.Angle)
		}
		return PointsBounds(ps)
	}
	if e.Kind == KindEllipse && e.Angle != 0 {
		si, co := math.Sincos(e.Angle)
		rx, ry := e.W/2, e.H/2
		w := math.Sqrt(rx*rx*co*co + ry*ry*si*si)
		h := math.Sqrt(rx*rx*si*si + ry*ry*co*co)
		return Box{e.X + rx - w, e.Y + ry - h, 2 * w, 2 * h}
	}
	return PointsBounds(WorldPoints(e))
}
func SceneBounds(s *Scene) Box {
	var b Box
	first := true
	for _, e := range s.Elements {
		if e == nil || e.IsDeleted {
			continue
		}
		if first {
			b = ElementBounds(e)
			first = false
		} else {
			b = Union(b, ElementBounds(e))
		}
	}
	for _, group := range s.GroupRegions() {
		b = Union(b, group.Bounds)
	}
	return b
}
func SelectionBounds(s *Scene, ids []string) Box {
	var b Box
	first := true
	for _, id := range ids {
		e := s.ElementByID(id)
		if e == nil || e.IsDeleted {
			continue
		}
		if first {
			b = ElementBounds(e)
			first = false
		} else {
			b = Union(b, ElementBounds(e))
		}
	}
	return b
}
func pointInPolygon(p Point, ps []Point) bool {
	inside := false
	for i, j := 0, len(ps)-1; i < len(ps); j, i = i, i+1 {
		a, b := ps[i], ps[j]
		if SegmentDistance(p, a, b) < 1e-8 {
			return true
		}
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}
func HitTest(e *Element, p Point, tolerance float64) bool {
	if e == nil || e.IsDeleted || !ElementBounds(e).Expand(tolerance+e.StrokeWidth/2).Contains(p) {
		return false
	}
	if e.Kind == KindFreedraw {
		q := Sub(RotatePoint(p, Point{e.X + e.W/2, e.Y + e.H/2}, -e.Angle), Point{e.X, e.Y})
		ps := FreehandOutline(e)
		if pointInPolygon(q, ps) {
			return true
		}
		for i := 1; i < len(ps); i++ {
			if SegmentDistance(q, ps[i-1], ps[i]) <= tolerance {
				return true
			}
		}
		return false
	}
	if e.IsLinear() || e.Kind == KindFreedraw {
		ps := WorldPoints(e)
		for i := 1; i < len(ps); i++ {
			if SegmentDistance(p, ps[i-1], ps[i]) <= tolerance+e.StrokeWidth/2 {
				return true
			}
		}
		return e.Polygon && pointInPolygon(p, ps)
	}
	q := RotatePoint(p, Point{e.X + e.W/2, e.Y + e.H/2}, -e.Angle)
	if e.Kind == KindEllipse {
		rx, ry := e.W/2+tolerance, e.H/2+tolerance
		if rx <= 0 || ry <= 0 {
			return false
		}
		dx, dy := (q[0]-e.X-e.W/2)/rx, (q[1]-e.Y-e.H/2)/ry
		return dx*dx+dy*dy <= 1
	}
	if e.Kind == KindDiamond {
		return pointInPolygon(p, WorldPoints(e))
	}
	return (Box{e.X, e.Y, e.W, e.H}).Expand(tolerance).Contains(q)
}
func (s *Scene) Hit(p Point, tolerance float64, includeLocked bool) *Element {
	for i := len(s.Elements) - 1; i >= 0; i-- {
		e := s.Elements[i]
		if e == nil || (!includeLocked && e.Locked) {
			continue
		}
		if HitTest(e, p, tolerance) {
			return e
		}
	}
	return nil
}
func (s *Scene) Lasso(ps []Point, intersect bool) []string {
	var out []string
	if len(ps) < 3 {
		return out
	}
	bounds := PointsBounds(ps)
	for _, e := range s.Elements {
		if e.IsDeleted || e.Locked || !bounds.Intersects(ElementBounds(e)) {
			continue
		}
		points := WorldPoints(e)
		all, any := true, false
		for _, p := range points {
			v := pointInPolygon(p, ps)
			all = all && v
			any = any || v
		}
		if all || (intersect && (any || HitTest(e, ps[0], 0))) {
			out = append(out, e.ID)
		}
	}
	return out
}

// Snap returns the displacement to nearby edges and centers in scene units.
func (s *Scene) Snap(ids []string, delta Point, threshold, grid float64) (Point, []Point) {
	b := SelectionBounds(s, ids)
	b.X += delta[0]
	b.Y += delta[1]
	chosen := idSet(ids)
	bestX, bestY := threshold, threshold
	dx, dy := float64(0), float64(0)
	var guides []Point
	if grid > 0 {
		dx = math.Round(b.X/grid)*grid - b.X
		dy = math.Round(b.Y/grid)*grid - b.Y
		bestX = math.Abs(dx)
		bestY = math.Abs(dy)
	}
	for _, e := range s.Elements {
		if e.IsDeleted || chosen[e.ID] {
			continue
		}
		r := ElementBounds(e)
		for _, x := range []float64{b.X, b.X + b.W/2, b.X + b.W} {
			for _, X := range []float64{r.X, r.X + r.W/2, r.X + r.W} {
				if d := X - x; math.Abs(d) < bestX {
					bestX = math.Abs(d)
					dx = d
					guides = append(guides, Point{X, math.NaN()})
				}
			}
		}
		for _, y := range []float64{b.Y, b.Y + b.H/2, b.Y + b.H} {
			for _, Y := range []float64{r.Y, r.Y + r.H/2, r.Y + r.H} {
				if d := Y - y; math.Abs(d) < bestY {
					bestY = math.Abs(d)
					dy = d
					guides = append(guides, Point{math.NaN(), Y})
				}
			}
		}
	}
	return Point{delta[0] + dx, delta[1] + dy}, guides
}
