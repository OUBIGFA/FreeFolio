package app

// Native Go port of thinking-orbs v0.3.1 (Jakub Antalik, MIT), pinned in
// third_party/thinking-orbs. Renders honestly 3D, depth-shaded, z-sorted
// dotted thought-orbs with plain 2D vector circles and lines — no bitmaps,
// no webview, no shader filters.

import (
	"cmp"
	"math"
	"slices"
)

// qaOrbStyle selects one of the ported Thinking Orbs visual styles.
type qaOrbStyle uint8

const (
	// orbWorking is upstream "working" (orbits): tilted 3D orbits with running particles.
	orbWorking qaOrbStyle = iota
	// orbSolving is upstream "solving" (rubik): lattice sphere with quarter-turn band twists.
	orbSolving
	// orbThinking is upstream "composing" (ribbon, labeled "Thinking" on libraries.dev/orbs):
	// undulating sash of parallel dotted strands.
	orbThinking
	// orbAgentThinking is upstream "breathing" (ring, labeled "Agent thinking" on libraries.dev/orbs):
	// face-on undulating morphing dot ring.
	orbAgentThinking
	// orbConnecting is upstream "connecting" (web, also labeled "Solving" on libraries.dev/orbs):
	// drifting constellation with proximity edges and traveling packets.
	orbConnecting
	orbStyleCount
)

func (s qaOrbStyle) next() qaOrbStyle {
	return (s + 1) % orbStyleCount
}

type orbDot struct {
	x, y, z float64
	r       float64
	// white is ink value: 0 = darkest ink on light paper, mirrored on dark themes.
	white float64
	a     float64
}

type orbLine struct {
	x1, y1, x2, y2 float64
	white          float64
	a              float64
	w              float64
}

type orbFrame struct {
	dots  []orbDot
	lines []orbLine
}

type orbOpts struct {
	// Orbits
	orbitN     int
	ghostN     int
	ghostR     float64
	ghostA     float64
	particles  int
	partR      float64
	partRDepth float64

	// Rubik lattice
	latRings   int
	lonDensity int
	moveCount  int
	rBase      float64
	rDepth     float64
	rActive    float64
	inkFar     float64
	inkSpan    float64

	// Ribbon / Ring
	lanes   int
	segs    int
	faceOn  bool
	spin    float64
	bandMul float64
	wobMul  float64

	// Web
	nodeN      int
	thr        float64
	signals    int
	nodeR      float64
	nodeRDepth float64
	lineW      float64
	spread     float64

	// Shared
	rsPow float64
	rMin  float64
}

type orbPreset struct {
	speed float64
	opts  orbOpts
}

type orbProj func(x, y, z float64) (float64, float64, float64)

func orbLerp(a, b, f float64) float64 { return a + (b-a)*f }

func orbFrac(x float64) float64 { return x - math.Floor(x) }

func orbHashD(a, b float64) float64 {
	h := math.Sin(a*12.9898+b*78.233) * 43758.5453
	return h - math.Floor(h)
}

func orbVNoise(x, y float64) float64 {
	xi := math.Floor(x)
	yi := math.Floor(y)
	fx := x - xi
	fy := y - yi
	fx = fx * fx * (3 - 2*fx)
	fy = fy * fy * (3 - 2*fy)
	a := orbHashD(xi, yi)
	b := orbHashD(xi+1, yi)
	c := orbHashD(xi, yi+1)
	d := orbHashD(xi+1, yi+1)
	return a + (b-a)*fx + (c-a)*fy + (a-b-c+d)*fx*fy
}

func orbFibDir(i, n int) (float64, float64, float64) {
	golden := math.Pi * (3 - math.Sqrt(5))
	y := 1 - (2*(float64(i)+0.5))/float64(n)
	rad := math.Sqrt(1 - y*y)
	a := float64(i) * golden
	return rad * math.Cos(a), y, rad * math.Sin(a)
}

func orbMakeProj(yaw, tilt, cx, cy, scale float64) orbProj {
	st, ct := math.Sin(tilt), math.Cos(tilt)
	sy, cyw := math.Sin(yaw), math.Cos(yaw)
	return func(x, y, z float64) (float64, float64, float64) {
		x1 := x*cyw + z*sy
		z1 := -x*sy + z*cyw
		y1 := y*ct - z1*st
		z2 := y*st + z1*ct
		return cx + x1*scale, cy - y1*scale, z2
	}
}

func orbRadiusScale(size, pow float64) float64 {
	return math.Pow(size/300, pow)
}

func orbFinalizeFrame(dots []orbDot, lines []orbLine, rMin float64) orbFrame {
	if rMin <= 0 {
		rMin = 0.3
	}
	visible := dots[:0]
	for _, d := range dots {
		if d.a < 0.02 {
			continue
		}
		if d.r < rMin {
			d.r = rMin
		}
		visible = append(visible, d)
	}
	slices.SortStableFunc(visible, func(a, b orbDot) int {
		return cmp.Compare(a.z, b.z)
	})
	visLines := lines[:0]
	for _, l := range lines {
		if l.a >= 0.02 {
			visLines = append(visLines, l)
		}
	}
	return orbFrame{dots: visible, lines: visLines}
}

// resolveOrbPreset mirrors upstream src/presets.ts + src/engine/profiles.ts
// for canonical sizes 20 and 64.
func resolveOrbPreset(style qaOrbStyle, canonicalSize int) orbPreset {
	small := canonicalSize <= 20
	scalePair := func(a, b int, scale float64) (int, int) {
		rt := math.Sqrt(scale)
		return max(2, int(math.Round(float64(a)*rt))), max(2, int(math.Round(float64(b)*rt)))
	}
	scaleCount := func(v int, scale float64) int {
		if v == 0 {
			return 0
		}
		return max(1, int(math.Round(float64(v)*scale)))
	}

	switch style {
	case orbWorking:
		o := orbOpts{
			orbitN: 12, ghostN: 40, ghostR: 0.9, ghostA: 0.5,
			particles: 3, partR: 1.2, partRDepth: 1.6,
			rsPow: 0.6, rMin: 0.3,
		}
		if small {
			o.orbitN = scaleCount(o.orbitN, 0.238)
			o.ghostN = scaleCount(o.ghostN, 0.238)
			o.ghostR *= 2.4
			o.partR *= 2.4
			o.partRDepth *= 2.4
			return orbPreset{speed: 3.9, opts: o}
		}
		return orbPreset{speed: 1.885, opts: o}

	case orbSolving:
		o := orbOpts{
			latRings: 15, lonDensity: 40, moveCount: 14,
			rBase: 0.6, rDepth: 1.7, rActive: 0.3,
			inkFar: 0.62, inkSpan: 0.54,
			rsPow: 0.6, rMin: 0.3,
		}
		if small {
			o.latRings, o.lonDensity = scalePair(o.latRings, o.lonDensity, 0.088)
			o.rBase *= 1.9
			o.rDepth *= 1.9
			o.rActive *= 1.9
			return orbPreset{speed: 1.95, opts: o}
		}
		o.latRings, o.lonDensity = scalePair(o.latRings, o.lonDensity, 0.35)
		o.rBase *= 1.05
		o.rDepth *= 1.05
		o.rActive *= 1.05
		return orbPreset{speed: 1.82, opts: o}

	case orbThinking:
		o := orbOpts{
			lanes: 5, segs: 88, ghostN: 150,
			rBase: 1.1, rDepth: 1.7,
			spin: 0, wobMul: 1,
			rsPow: 0.6, rMin: 0.3,
		}
		if small {
			o.lanes, o.segs = scalePair(o.lanes, o.segs, 0.051)
			o.ghostN = scaleCount(o.ghostN, 0.051)
			o.rBase *= 1.073
			o.rDepth *= 1.073
			o.bandMul = 4.94
			return orbPreset{speed: 3.12, opts: o}
		}
		o.lanes, o.segs = scalePair(o.lanes, o.segs, 0.25)
		o.ghostN = scaleCount(o.ghostN, 0.25)
		o.rBase *= 0.85
		o.rDepth *= 0.85
		o.bandMul = 3.9
		return orbPreset{speed: 2.34, opts: o}

	case orbAgentThinking:
		o := orbOpts{
			lanes: 5, segs: 88, ghostN: 0, faceOn: true,
			rBase: 1.1, rDepth: 1.7,
			spin:  0,
			rsPow: 0.6, rMin: 0.3,
		}
		if small {
			o.lanes, o.segs = scalePair(o.lanes, o.segs, 0.028)
			o.rBase *= 1.622
			o.rDepth *= 1.622
			o.bandMul = 3.968
			o.wobMul = 0.565
			return orbPreset{speed: 3.78, opts: o}
		}
		o.lanes, o.segs = scalePair(o.lanes, o.segs, 0.25)
		o.rBase *= 0.956
		o.rDepth *= 0.956
		o.bandMul = 3.627
		o.wobMul = 0.368
		return orbPreset{speed: 3.24, opts: o}

	case orbConnecting:
		o := orbOpts{
			nodeN: 30, thr: 0.72, signals: 5,
			nodeR: 1.4, nodeRDepth: 1.8, lineW: 0.8, spread: 1,
			rsPow: 0.6, rMin: 0.3,
		}
		if small {
			o.nodeN = scaleCount(o.nodeN, 0.25)
			o.signals = scaleCount(o.signals, 0.25)
			o.nodeR *= 1.52
			o.nodeRDepth *= 1.52
			return orbPreset{speed: 6.63, opts: o}
		}
		o.nodeN = scaleCount(o.nodeN, 1.35)
		o.signals = scaleCount(o.signals, 1.35)
		o.nodeR *= 0.95
		o.nodeRDepth *= 0.95
		return orbPreset{speed: 3.315, opts: o}
	}
	return resolveOrbPreset(orbWorking, canonicalSize)
}

func orbComputeFrame(style qaOrbStyle, size, t float64, o orbOpts) orbFrame {
	switch style {
	case orbWorking:
		return orbFrameOrbits(size, t, o)
	case orbSolving:
		return orbFrameRubik(size, t, o)
	case orbThinking, orbAgentThinking:
		return orbFrameRibbon(size, t, o)
	case orbConnecting:
		return orbFrameWeb(size, t, o)
	default:
		return orbFrameOrbits(size, t, o)
	}
}

func orbFrameOrbits(size, t float64, o orbOpts) orbFrame {
	cx, cy := size/2, size/2
	R := (size / 2) * 0.82
	pt := orbMakeProj(t*0.12, 0.3, cx, cy, 1)
	rs := orbRadiusScale(size, o.rsPow)

	dots := make([]orbDot, 0, o.orbitN*(o.ghostN+o.particles))
	for orb := range o.orbitN {
		h1 := orbHashD(float64(orb), 1.7)
		h2 := orbHashD(float64(orb), 5.2)
		h3 := orbHashD(float64(orb), 8.9)
		ro := R * (0.45 + 0.52*h1)
		th := h1 * 2 * math.Pi
		phi := math.Acos(2*h2 - 1)
		nx := math.Sin(phi) * math.Cos(th)
		ny := math.Cos(phi)
		nz := math.Sin(phi) * math.Sin(th)
		ux, uy, uz := -ny, nx, 0.0
		ul := max(1e-6, math.Sqrt(ux*ux+uy*uy))
		ux /= ul
		uy /= ul
		vx := ny*uz - nz*uy
		vy := nz*ux - nx*uz
		vz := nx*uy - ny*ux
		dir := -1.0
		if h3 > 0.5 {
			dir = 1.0
		}
		speed := (0.25 + 0.55*h3) * dir

		for k := range o.ghostN {
			a := (float64(k) / float64(o.ghostN)) * 2 * math.Pi
			ca, sa := math.Cos(a), math.Sin(a)
			px, py, z := pt((ux*ca+vx*sa)*ro, (uy*ca+vy*sa)*ro, (uz*ca+vz*sa)*ro)
			depth := (z/ro + 1) / 2
			dots = append(dots, orbDot{
				x: px, y: py, z: z,
				r:     o.ghostR * rs,
				white: 0.72,
				a:     o.ghostA * (0.4 + 0.6*depth),
			})
		}
		for m := range o.particles {
			a := t*speed + (float64(m)/float64(o.particles))*2*math.Pi + h2*6
			ca, sa := math.Cos(a), math.Sin(a)
			px, py, z := pt((ux*ca+vx*sa)*ro, (uy*ca+vy*sa)*ro, (uz*ca+vz*sa)*ro)
			depth := (z/ro + 1) / 2
			dots = append(dots, orbDot{
				x: px, y: py, z: z,
				r:     (o.partR + o.partRDepth*depth) * rs,
				white: 0.3 - 0.22*depth,
				a:     1,
			})
		}
	}
	return orbFinalizeFrame(dots, nil, o.rMin)
}

type orbRubikMove struct {
	axis int
	lo   float64
	hi   float64
	ang  float64
}

func orbRubikMoves(count int) []orbRubikMove {
	moves := make([]orbRubikMove, count)
	for i := range count {
		axis := min(2, int(math.Floor(orbHashD(float64(i), 2.3)*3)))
		lo := -1.0 + 0.5*float64(min(3, int(math.Floor(orbHashD(float64(i), 5.9)*4))))
		dir := -1.0
		if orbHashD(float64(i), 7.7) < 0.5 {
			dir = 1.0
		}
		moves[i] = orbRubikMove{axis: axis, lo: lo, hi: lo + 0.5, ang: dir * math.Pi / 2}
	}
	return moves
}

func orbSolveCycle(t float64, count int, slotDur, rest float64) ([]float64, int) {
	cyc := 2*float64(count)*slotDur + rest
	tc := math.Mod(t, cyc)
	if tc < 0 {
		tc += cyc
	}
	amount := make([]float64, count)
	active := -1
	if tc < 2*float64(count)*slotDur {
		slot := int(math.Floor(tc / slotDur))
		p := (tc - float64(slot)*slotDur) / slotDur
		cl := min(1, p/0.7)
		ep := 1 - math.Pow(1-cl, 3)
		if slot < count {
			for i := range slot {
				amount[i] = 1
			}
			amount[slot] = ep
			active = slot
		} else {
			u := 2*count - 1 - slot
			for i := range u {
				amount[i] = 1
			}
			amount[u] = 1 - ep
			active = u
		}
	}
	return amount, active
}

func orbFrameRubik(size, t float64, o orbOpts) orbFrame {
	cx, cy := size/2, size/2
	R := (size / 2) * 0.82
	pt := orbMakeProj(t*0.55, 0.35+0.1*math.Sin(t*0.9), cx, cy, R)
	rs := orbRadiusScale(size, o.rsPow)
	moves := orbRubikMoves(o.moveCount)
	amount, active := orbSolveCycle(t, o.moveCount, 0.42, 1.2)

	dots := make([]orbDot, 0, (o.latRings+1)*o.lonDensity)
	for li := 0; li <= o.latRings; li++ {
		lat := -math.Pi/2 + (float64(li)/float64(o.latRings))*math.Pi
		cosLat, sinLat := math.Cos(lat), math.Sin(lat)
		lonCount := max(1, int(math.Round(math.Abs(cosLat)*float64(o.lonDensity))))
		for lj := range lonCount {
			lon := (float64(lj) / float64(lonCount)) * 2 * math.Pi
			x, y, z := cosLat*math.Cos(lon), sinLat, cosLat*math.Sin(lon)
			inActive := false
			for i, mv := range moves {
				if amount[i] <= 0 {
					continue
				}
				var coord float64
				switch mv.axis {
				case 0:
					coord = x
				case 1:
					coord = y
				default:
					coord = z
				}
				if coord < mv.lo || coord >= mv.hi {
					continue
				}
				if i == active {
					inActive = true
				}
				a := mv.ang * amount[i]
				ca, sa := math.Cos(a), math.Sin(a)
				switch mv.axis {
				case 0:
					y2 := y*ca - z*sa
					z = y*sa + z*ca
					y = y2
				case 1:
					x2 := x*ca + z*sa
					z = -x*sa + z*ca
					x = x2
				default:
					x2 := x*ca - y*sa
					y = x*sa + y*ca
					x = x2
				}
			}
			px, py, zr := pt(x, y, z)
			depth := (zr + 1) / 2
			r := (o.rBase + o.rDepth*depth)
			white := o.inkFar - o.inkSpan*depth
			if inActive {
				r += o.rActive
				white -= 0.14
			}
			dots = append(dots, orbDot{x: px, y: py, z: zr, r: r * rs, white: white, a: 1})
		}
	}
	return orbFinalizeFrame(dots, nil, o.rMin)
}

func orbFrameRibbon(size, t float64, o orbOpts) orbFrame {
	cx, cy := size/2, size/2
	R := (size / 2) * 0.78
	camTilt := 0.3
	pt := orbMakeProj(t*0.1*o.spin, camTilt, cx, cy, 1)
	rs := orbRadiusScale(size, o.rsPow)

	lanes := max(1, int(math.Round(float64(o.lanes)*o.bandMul)))
	dots := make([]orbDot, 0, o.ghostN+lanes*o.segs)
	for i := range o.ghostN {
		dx, dy, dz := orbFibDir(i, o.ghostN)
		px, py, z := pt(dx*R, dy*R, dz*R)
		depth := (z/R + 1) / 2
		dots = append(dots, orbDot{x: px, y: py, z: z, r: 0.8 * rs, white: 0.78, a: 0.1 + 0.22*depth})
	}

	ya := t * 0.24 * o.spin
	ta := 0.55 + 0.3*math.Sin(t*0.18)*o.spin
	if o.faceOn {
		ta = -camTilt
	}
	ux, uy, uz := math.Cos(ya), 0.0, math.Sin(ya)
	vx, vy, vz := -uz*math.Sin(ta), math.Cos(ta), ux*math.Sin(ta)
	nx := uy*vz - uz*vy
	ny := uz*vx - ux*vz
	nz := ux*vy - uy*vx

	wobAmp := 0.23 * o.wobMul
	baseR := R
	if o.faceOn {
		baseR = R / (1 + 0.85*wobAmp)
	}

	halfSpan := max(1.0, float64(lanes-1)/2)
	for w := range lanes {
		laneOff := (float64(w) - float64(lanes-1)/2) * 0.075
		edge := math.Abs(float64(w)-float64(lanes-1)/2) / halfSpan
		for k := range o.segs {
			a := (float64(k) / float64(o.segs)) * 2 * math.Pi
			wob := (0.16*math.Sin(a*3-t*1.7+float64(w)*0.22) + 0.07*math.Sin(a*5+t*1.1)) * o.wobMul
			radial := 1.0
			off := laneOff + wob
			if o.faceOn {
				radial = 1 + wob
				off = laneOff
			}
			ca, sa := math.Cos(a), math.Sin(a)
			x := ux*ca + vx*sa + nx*off
			y := uy*ca + vy*sa + ny*off
			z := uz*ca + vz*sa + nz*off
			l := math.Sqrt(x*x + y*y + z*z)
			rr := baseR * radial
			px, py, zr := pt((x/l)*rr, (y/l)*rr, (z/l)*rr)
			depth := (zr/R + 1) / 2
			dots = append(dots, orbDot{
				x: px, y: py, z: zr,
				r:     (o.rBase + o.rDepth*depth) * (1 - 0.25*edge) * rs,
				white: 0.52 - 0.44*depth + 0.18*edge,
				a:     0.4 + 0.6*depth,
			})
		}
	}
	return orbFinalizeFrame(dots, nil, o.rMin)
}

func orbFrameWeb(size, t float64, o orbOpts) orbFrame {
	cx, cy := size/2, size/2
	spread := o.spread
	if spread == 0 {
		spread = 1
	}
	R := (size / 2) * 0.8 * spread
	pt := orbMakeProj(t*0.12, 0.32, cx, cy, R)
	rs := orbRadiusScale(size, o.rsPow)

	nodes := make([][3]float64, o.nodeN)
	for i := range o.nodeN {
		dx, dy, dz := orbFibDir(i, o.nodeN)
		x := dx + 0.3*(orbVNoise(float64(i)*0.31+9, t*0.24)-0.5)*2
		y := dy + 0.3*(orbVNoise(float64(i)*0.53+27, t*0.21)-0.5)*2
		z := dz + 0.3*(orbVNoise(float64(i)*0.77+55, t*0.27)-0.5)*2
		l := math.Sqrt(x*x + y*y + z*z)
		nodes[i] = [3]float64{x / l, y / l, z / l}
	}

	var lines []orbLine
	for i := range o.nodeN {
		for j := i + 1; j < o.nodeN; j++ {
			dx := nodes[i][0] - nodes[j][0]
			dy := nodes[i][1] - nodes[j][1]
			dz := nodes[i][2] - nodes[j][2]
			dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
			if dist >= o.thr {
				continue
			}
			x1, y1, z1 := pt(nodes[i][0], nodes[i][1], nodes[i][2])
			x2, y2, z2 := pt(nodes[j][0], nodes[j][1], nodes[j][2])
			depth := ((z1+z2)/2 + 1) / 2
			lines = append(lines, orbLine{
				x1: x1, y1: y1, x2: x2, y2: y2,
				white: 0.42,
				a:     (1 - dist/o.thr) * (0.3 + 0.55*depth),
				w:     max(0.6, o.lineW*rs),
			})
		}
	}

	dots := make([]orbDot, 0, o.nodeN+o.signals)
	for i := range o.nodeN {
		px, py, z := pt(nodes[i][0], nodes[i][1], nodes[i][2])
		depth := (z + 1) / 2
		pulse := 1 + 0.25*math.Sin(t*1.4+float64(i)*2.7)
		dots = append(dots, orbDot{
			x: px, y: py, z: z,
			r:     (o.nodeR + o.nodeRDepth*depth) * pulse * rs,
			white: 0.55 - 0.45*depth,
			a:     1,
		})
	}

	for s := range o.signals {
		seg := math.Floor(t*0.55 + float64(s)*7.31)
		a := int(math.Floor(orbHashD(seg, float64(s)*3.1+1.7) * float64(o.nodeN)))
		b := int(math.Floor(orbHashD(seg, float64(s)*5.7+4.2) * float64(o.nodeN)))
		if a == b {
			continue
		}
		f := orbFrac(t*0.55 + float64(s)*7.31)
		x := orbLerp(nodes[a][0], nodes[b][0], f)
		y := orbLerp(nodes[a][1], nodes[b][1], f)
		z := orbLerp(nodes[a][2], nodes[b][2], f)
		l := max(1e-6, math.Sqrt(x*x+y*y+z*z))
		px, py, zr := pt(x/l, y/l, z/l)
		depth := (zr + 1) / 2
		dots = append(dots, orbDot{
			x: px, y: py, z: zr,
			r:     (o.nodeR*1.5 + o.nodeRDepth*depth) * rs,
			white: 0.05,
			a:     0.5 + 0.5*depth,
		})
	}
	return orbFinalizeFrame(dots, lines, o.rMin)
}
