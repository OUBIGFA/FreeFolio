package whiteboard

import (
	"math"
	"sort"
)

// RouteElbow uses the non-uniform orthogonal grid described by Quiet.md's
// elbow router. Bound-node rectangles are obstacles and turns carry a cost;
// fixed segments remain waypoints while their endpoints move.
func (s *Scene) RouteElbow(a *Element, start, end Point) []Point {
	var obstacles []Box
	stub := func(p Point, b *Binding) Point {
		if b == nil {
			return p
		}
		target := s.ElementByID(b.ElementID)
		if target == nil {
			return p
		}
		box := ElementBounds(target)
		obstacles = append(obstacles, box.Expand(6))
		dist := []float64{math.Abs(p[0] - box.X), math.Abs(p[0] - box.X - box.W), math.Abs(p[1] - box.Y), math.Abs(p[1] - box.Y - box.H)}
		side := 0
		for i := 1; i < 4; i++ {
			if dist[i] < dist[side] {
				side = i
			}
		}
		switch side {
		case 0:
			return Point{box.X - 40, p[1]}
		case 1:
			return Point{box.X + box.W + 40, p[1]}
		case 2:
			return Point{p[0], box.Y - 40}
		default:
			return Point{p[0], box.Y + box.H + 40}
		}
	}
	first, last := stub(start, a.StartBinding), stub(end, a.EndBinding)
	waypoints := []Point{first}
	for _, fixed := range a.FixedSegments {
		waypoints = append(waypoints, Add(start, fixed.Start), Add(start, fixed.End))
	}
	waypoints = append(waypoints, last)
	world := []Point{start}
	for i := 1; i < len(waypoints); i++ {
		route := orthogonalRoute(waypoints[i-1], waypoints[i], obstacles)
		if len(route) == 0 {
			route = []Point{waypoints[i-1], {waypoints[i][0], waypoints[i-1][1]}, waypoints[i]}
		}
		world = append(world, route...)
	}
	world = append(world, end)
	var out []Point
	for _, p := range world {
		p = Sub(p, start)
		if len(out) > 0 && Distance(p, out[len(out)-1]) < 1e-8 {
			continue
		}
		if len(out) > 1 {
			a, b := out[len(out)-2], out[len(out)-1]
			if (a[0] == b[0] && b[0] == p[0]) || (a[1] == b[1] && b[1] == p[1]) {
				out[len(out)-1] = p
				continue
			}
		}
		out = append(out, p)
	}
	return out
}
func orthogonalRoute(start, end Point, obstacles []Box) []Point {
	xs, ys := []float64{start[0], end[0]}, []float64{start[1], end[1]}
	for _, b := range obstacles {
		xs = append(xs, b.X-1, b.X+b.W+1)
		ys = append(ys, b.Y-1, b.Y+b.H+1)
	}
	sort.Float64s(xs)
	sort.Float64s(ys)
	unique := func(v []float64) []float64 {
		out := v[:0]
		for _, x := range v {
			if len(out) == 0 || math.Abs(x-out[len(out)-1]) > 1e-8 {
				out = append(out, x)
			}
		}
		return out
	}
	xs, ys = unique(xs), unique(ys)
	nx, ny := len(xs), len(ys)
	point := func(i int) Point { return Point{xs[i%nx], ys[i/nx]} }
	index := func(p Point) int { return sort.SearchFloat64s(ys, p[1])*nx + sort.SearchFloat64s(xs, p[0]) }
	startID, endID := index(start), index(end)
	valid := func(a, b Point) bool {
		for _, r := range obstacles {
			if a[0] == b[0] {
				if a[0] > r.X && a[0] < r.X+r.W && math.Max(a[1], b[1]) > r.Y && math.Min(a[1], b[1]) < r.Y+r.H {
					return false
				}
			} else if a[1] > r.Y && a[1] < r.Y+r.H && math.Max(a[0], b[0]) > r.X && math.Min(a[0], b[0]) < r.X+r.W {
				return false
			}
		}
		return true
	}
	count := nx * ny * 2
	cost := make([]float64, count)
	prev := make([]int, count)
	used := make([]bool, count)
	for i := range cost {
		cost[i] = math.Inf(1)
		prev[i] = -1
	}
	cost[startID*2], cost[startID*2+1] = 0, 0
	bestEnd := -1
	for range count {
		at := -1
		best := math.Inf(1)
		for i, c := range cost {
			if !used[i] && c < best {
				at, best = i, c
			}
		}
		if at < 0 {
			break
		}
		used[at] = true
		node, heading := at/2, at%2
		if node == endID {
			bestEnd = at
			break
		}
		x, y := node%nx, node/nx
		neighbors := []struct{ x, y, dir int }{{x - 1, y, 0}, {x + 1, y, 0}, {x, y - 1, 1}, {x, y + 1, 1}}
		for _, n := range neighbors {
			if n.x < 0 || n.x >= nx || n.y < 0 || n.y >= ny {
				continue
			}
			next := n.y*nx + n.x
			if !valid(point(node), point(next)) {
				continue
			}
			v := best + Distance(point(node), point(next))
			if heading != n.dir {
				v += 20
			}
			target := next*2 + n.dir
			if v < cost[target] {
				cost[target] = v
				prev[target] = at
			}
		}
	}
	if bestEnd < 0 {
		return nil
	}
	var reverse []Point
	for at := bestEnd; at >= 0; at = prev[at] {
		reverse = append(reverse, point(at/2))
	}
	out := make([]Point, len(reverse))
	for i := range reverse {
		out[len(reverse)-1-i] = reverse[i]
	}
	return out
}
