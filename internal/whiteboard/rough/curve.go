// Curve sampling follows points-on-curve 0.2.0/1.0.1 (MIT, Preet Shihn).
package rough

import "math"

func lerp(a, b Point, t float64) Point { return Point{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t} }
func distSq(a, b Point) float64        { return (a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) }
func segmentDistSq(p, a, b Point) float64 {
	l := distSq(a, b)
	if l == 0 {
		return distSq(p, a)
	}
	t := ((p[0]-a[0])*(b[0]-a[0]) + (p[1]-a[1])*(b[1]-a[1])) / l
	return distSq(p, lerp(a, b, math.Max(0, math.Min(1, t))))
}
func flatness(p []Point) float64 {
	ux := 3*p[1][0] - 2*p[0][0] - p[3][0]
	uy := 3*p[1][1] - 2*p[0][1] - p[3][1]
	vx := 3*p[2][0] - 2*p[3][0] - p[0][0]
	vy := 3*p[2][1] - 2*p[3][1] - p[0][1]
	return math.Max(ux*ux, vx*vx) + math.Max(uy*uy, vy*vy)
}
func splitBezier(p []Point, tolerance float64, out *[]Point) {
	if flatness(p) < tolerance {
		if len(*out) == 0 || math.Sqrt(distSq((*out)[len(*out)-1], p[0])) > 1 {
			*out = append(*out, p[0])
		}
		*out = append(*out, p[3])
		return
	}
	q1, q2, q3 := lerp(p[0], p[1], .5), lerp(p[1], p[2], .5), lerp(p[2], p[3], .5)
	r1, r2 := lerp(q1, q2, .5), lerp(q2, q3, .5)
	red := lerp(r1, r2, .5)
	splitBezier([]Point{p[0], q1, r1, red}, tolerance, out)
	splitBezier([]Point{red, r2, q3, p[3]}, tolerance, out)
}
func simplifyPoints(ps []Point, eps float64, out *[]Point) {
	if len(ps) == 0 {
		return
	}
	s, e := ps[0], ps[len(ps)-1]
	maxSq, at := float64(0), 1
	for i := 1; i < len(ps)-1; i++ {
		d := segmentDistSq(ps[i], s, e)
		if d > maxSq {
			maxSq = d
			at = i
		}
	}
	if math.Sqrt(maxSq) > eps {
		simplifyPoints(ps[:at+1], eps, out)
		simplifyPoints(ps[at:], eps, out)
	} else {
		if len(*out) == 0 {
			*out = append(*out, s)
		}
		*out = append(*out, e)
	}
}
func pointsOnBezier(ps []Point, tolerance, distance float64) []Point {
	var out []Point
	for i := 0; i+3 < len(ps); i += 3 {
		splitBezier(ps[i:i+4], tolerance, &out)
	}
	if distance > 0 && len(out) > 0 {
		var simp []Point
		simplifyPoints(out, distance, &simp)
		return simp
	}
	return out
}
func curveToBezier(ps []Point) []Point {
	if len(ps) < 3 {
		return append([]Point{}, ps...)
	}
	if len(ps) == 3 {
		return []Point{ps[0], ps[1], ps[2], ps[2]}
	}
	p := []Point{ps[0], ps[0]}
	p = append(p, ps[1:]...)
	p = append(p, ps[len(ps)-1])
	out := []Point{p[0]}
	for i := 1; i < len(p)-2; i++ {
		c := p[i]
		out = append(out, Point{c[0] + (p[i+1][0]-p[i-1][0])/6, c[1] + (p[i+1][1]-p[i-1][1])/6}, Point{p[i+1][0] + (p[i][0]-p[i+2][0])/6, p[i+1][1] + (p[i][1]-p[i+2][1])/6}, p[i+1])
	}
	return out
}
