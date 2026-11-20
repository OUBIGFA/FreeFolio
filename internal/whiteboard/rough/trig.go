// The following kernels and moderate-argument reduction are adapted from
// fdlibm (https://www.netlib.org/fdlibm/), k_sin.c, k_cos.c and e_rem_pio2.c.
// Copyright (C) 1993 by Sun Microsystems, Inc. All rights reserved.
// Developed at SunSoft, a Sun Microsystems, Inc. business.
// Permission to use, copy, modify, and distribute this software is freely
// granted, provided that this notice is preserved.
package rough

import "math"

// Scanline rotation needs the reference trig rounding: a one-ULP difference
// can change floor(length / dashPeriod) and the entire following random stream.
// Ordinary geometry can use Go's math functions without this discrete effect.
func referenceSincos(x float64) (float64, float64) {
	if math.Abs(x) > 1e5 {
		return math.Sincos(x)
	}
	x0, y, n := reducePi(x)
	s, c := kernelSin(x0, y, n != 0), kernelCos(x0, y)
	switch n & 3 {
	case 0:
		return s, c
	case 1:
		return c, -s
	case 2:
		return -s, -c
	default:
		return -c, s
	}
}
func reducePi(x float64) (float64, float64, int) {
	const p1 = 1.57079632673412561417
	const p1t = 6.07710050650619224932e-11
	const p2 = 6.07710050630396597660e-11
	const p2t = 2.02226624879595063154e-21
	const p3 = 2.02226624871116645580e-21
	const p3t = 8.47842766036889956997e-32
	a := math.Abs(x)
	hi := uint32(math.Float64bits(a) >> 32)
	if hi <= 0x3fe921fb {
		return x, 0, 0
	}
	if hi < 0x4002d97c {
		z := a - p1
		var y0, y1 float64
		if hi != 0x3ff921fb {
			y0 = z - p1t
			y1 = (z - y0) - p1t
		} else {
			z -= p2
			y0 = z - p2t
			y1 = (z - y0) - p2t
		}
		if x < 0 {
			return -y0, -y1, -1
		}
		return y0, y1, 1
	}
	n := int(a*6.36619772367581382433e-1 + .5)
	fn := float64(n)
	r := a - fn*p1
	w := fn * p1t
	y0 := r - w
	j := int(hi >> 20)
	delta := j - int((math.Float64bits(y0)>>52)&0x7ff)
	if delta > 16 {
		t := r
		w = fn * p2
		r = t - w
		w = fn*p2t - ((t - r) - w)
		y0 = r - w
		delta = j - int((math.Float64bits(y0)>>52)&0x7ff)
		if delta > 49 {
			t = r
			w = fn * p3
			r = t - w
			w = fn*p3t - ((t - r) - w)
			y0 = r - w
		}
	}
	y1 := (r - y0) - w
	if x < 0 {
		return -y0, -y1, -n
	}
	return y0, y1, n
}
func kernelSin(x, y float64, tail bool) float64 {
	const s1 = -1.66666666666666324348e-1
	const s2 = 8.33333333332248946124e-3
	const s3 = -1.98412698298579493134e-4
	const s4 = 2.75573137070700676789e-6
	const s5 = -2.50507602534068634195e-8
	const s6 = 1.58969099521155010221e-10
	if math.Abs(x) < 0x1p-27 {
		return x
	}
	z := x * x
	v := z * x
	r := s2 + z*(s3+z*(s4+z*(s5+z*s6)))
	if !tail {
		return x + v*(s1+z*r)
	}
	return x - ((z*(.5*y-v*r) - y) - v*s1)
}
func kernelCos(x, y float64) float64 {
	const c1 = 4.16666666666666019037e-2
	const c2 = -1.38888888888741095749e-3
	const c3 = 2.48015872894767294178e-5
	const c4 = -2.75573143513906633035e-7
	const c5 = 2.08757232129817482790e-9
	const c6 = -1.13596475577881948265e-11
	hi := uint32(math.Float64bits(math.Abs(x)) >> 32)
	if hi < 0x3e400000 {
		return 1
	}
	z := x * x
	r := z * (c1 + z*(c2+z*(c3+z*(c4+z*(c5+z*c6)))))
	if hi < 0x3fd33333 {
		return 1 - (.5*z - (z*r - x*y))
	}
	qx := .28125
	if hi <= 0x3fe90000 {
		qx = math.Float64frombits(uint64(hi-0x00200000) << 32)
	}
	iz := .5*z - qx
	a := 1 - qx
	return a - (iz - (z*r - x*y))
}
