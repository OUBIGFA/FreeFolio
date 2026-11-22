package app

// Native Go port of LDRS (Griffin Johnston, MIT), pinned in third_party/ldrs:
// - Tail Chase (tailChase.ts / tailChase.scss)
// - Dot Stream (dotStream.ts / dotStream.scss)
// - Mirage (mirage.ts / mirage.scss)

import (
	"image"
	"math"

	"github.com/egoist/mygo/ui"
)

// qaLoaderStyle selects one of the three ported LDRS loader animations.
type qaLoaderStyle uint8

const (
	loaderTailChase qaLoaderStyle = iota
	loaderDotStream
	loaderMirage
	loaderStyleCount
)

func (s qaLoaderStyle) next() qaLoaderStyle {
	return (s + 1) % loaderStyleCount
}

type ldrsDot struct {
	x, y, r float64
}

var ldrsEaseInOut = ui.CubicBezier(0.42, 0, 0.58, 1)

func ldrsFrac(x float64) float64 {
	return x - math.Floor(x)
}

func ldrsTailChaseDots(size, tSec float64) [6]ldrsDot {
	const speed = 1.5
	dotSize := size * 0.17
	orbitR := (size - dotSize) / 2
	cx, cy := size/2, size/2
	baseAngle := (tSec / (speed * 1.8)) * 2 * math.Pi

	delays := [6]float64{
		0,
		-0.835 * 0.5 * speed,
		-0.668 * 0.5 * speed,
		-0.501 * 0.5 * speed,
		-0.334 * 0.5 * speed,
		-0.167 * 0.5 * speed,
	}
	var dots [6]ldrsDot
	for i, delay := range delays {
		p := ldrsFrac((tSec - delay) / speed)
		dotAngle := 2 * math.Pi
		if p < 0.65 {
			dotAngle = float64(ldrsEaseInOut(float32(p/0.65))) * 2 * math.Pi
		}
		ang := baseAngle + dotAngle - math.Pi/2
		dots[i] = ldrsDot{
			x: cx + orbitR*math.Cos(ang),
			y: cy + orbitR*math.Sin(ang),
			r: dotSize / 2,
		}
	}
	return dots
}

func ldrsStreamDots(size, tSec, dotRatio float64) [5]ldrsDot {
	const speed = 2.5
	dotSize := size * dotRatio
	cy := dotSize / 2
	delays := [5]float64{0, -0.2 * speed, -0.4 * speed, -0.6 * speed, -0.8 * speed}

	var dots [5]ldrsDot
	for i, delay := range delays {
		p := ldrsFrac((tSec - delay) / speed)
		scale := 2 * p
		if p > 0.5 {
			scale = 2 * (1 - p)
		}
		dots[i] = ldrsDot{
			x: size * p,
			y: cy,
			r: (dotSize / 2) * scale,
		}
	}
	return dots
}

func ldrsDotStreamDots(size, tSec float64) [5]ldrsDot {
	return ldrsStreamDots(size, tSec, 0.18)
}

func ldrsMirageDots(size, tSec float64) [5]ldrsDot {
	return ldrsStreamDots(size, tSec, 0.23)
}

// ldrsMiragePainter evaluates the #uib-jelly-ooze SVG filter (feGaussianBlur
// stdDeviation = size/20 plus feColorMatrix alpha transfer 18a - 7 blended with
// the crisp source circles) into a reusable 2x supersampled RGBA bitmap.
type ldrsMiragePainter struct {
	bitmap *ui.Bitmap
	pixels *image.RGBA
}

func (m *ldrsMiragePainter) render(size, tSec float64, col ui.Color, opacity float64) (*ui.Bitmap, float64, float64, float64) {
	const scaleFactor = 2.0
	height := size * 0.23
	sigma := size / 20.0
	pad := math.Ceil(sigma * 3.0)
	logicalW := size + 2*pad
	logicalH := height + 2*pad
	pxW := max(1, int(math.Ceil(logicalW*scaleFactor)))
	pxH := max(1, int(math.Ceil(logicalH*scaleFactor)))

	if m.pixels == nil || m.pixels.Bounds().Dx() != pxW || m.pixels.Bounds().Dy() != pxH {
		m.pixels = image.NewRGBA(image.Rect(0, 0, pxW, pxH))
		m.bitmap = nil
	}

	dots := ldrsMirageDots(size, tSec)
	invSqrt2Sigma := 1.0 / (sigma * math.Sqrt2)
	colAlpha := float64(col.A) / 255.0
	op := min(1.0, max(0.0, opacity))
	pix := m.pixels.Pix
	stride := m.pixels.Stride

	for py := range pxH {
		y := (float64(py)+0.5)/scaleFactor - pad
		row := py * stride
		for px := range pxW {
			x := (float64(px)+0.5)/scaleFactor - pad
			blurA := 0.0
			srcA := 0.0
			for _, d := range dots {
				if d.r <= 0.01 {
					continue
				}
				dist := math.Hypot(x-d.x, y-d.y)
				blurA += 0.5 * math.Erfc((dist-d.r)*invSqrt2Sigma)
				cov := (d.r + 0.35 - dist) / 0.7
				if cov > 0 {
					if cov > 1 {
						cov = 1
					}
					if cov > srcA {
						srcA = cov
					}
				}
			}
			if blurA > 1 {
				blurA = 1
			}
			ooze := 18.0*blurA - 7.0
			if ooze < 0 {
				ooze = 0
			} else if ooze > 1 {
				ooze = 1
			}
			alpha := max(ooze, srcA) * op * colAlpha
			off := row + px*4
			if alpha <= 0.001 {
				pix[off] = 0
				pix[off+1] = 0
				pix[off+2] = 0
				pix[off+3] = 0
				continue
			}
			// MyGo bitmaps use premultiplied RGBA.
			pix[off] = uint8(math.Round(float64(col.R) * alpha))
			pix[off+1] = uint8(math.Round(float64(col.G) * alpha))
			pix[off+2] = uint8(math.Round(float64(col.B) * alpha))
			pix[off+3] = uint8(math.Round(255.0 * alpha))
		}
	}

	if m.bitmap == nil {
		m.bitmap = ui.NewBitmap(m.pixels)
	} else {
		m.bitmap.Update(m.pixels)
	}
	return m.bitmap, pad, logicalW, logicalH
}

func (m *ldrsMiragePainter) draw(p *ui.Painter, box ui.Rect, size, tSec float64, col ui.Color, opacity float64) {
	bitmap, pad, logicalW, logicalH := m.render(size, tSec, col, opacity)
	height := size * 0.23
	ox := float64(box.X) + (float64(box.W)-size)/2 - pad
	oy := float64(box.Y) + (float64(box.H)-height)/2 - pad
	dst := ui.Rect{
		X: float32(ox),
		Y: float32(oy),
		W: float32(logicalW),
		H: float32(logicalH),
	}
	p.ImageRegion(bitmap, ui.Rect{}, dst, ui.ImageOptions{})
}
