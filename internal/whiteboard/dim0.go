// Geometry ported from dim0's canvas-harness 0.2.1 (MIT).
// See third_party/canvas-harness for the pinned source and parity fixtures.
package whiteboard

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard/rough"
)

var DimShapeNames = []string{"tag", "thought-cloud", "capsule", "layered-rect", "layered-ellipse", "layered-diamond", "soft-diamond"}

// NewDimShape keeps the standard rectangle exchange type; the extension names
// the richer native outline. All ordinary transform, text and history tools apply.
func NewDimShape(name string, x, y, w, h float64) *Element {
	e := NewElement(KindRectangle, x, y, w, h)
	e.Shape = name
	e.Roughness, e.StrokeWidth = 1, 2
	e.StrokeColor, e.FillColor, e.FillStyle = "#475569", "#dbeafe", FillSolid
	r := float64(12)
	e.Roundness = &Roundness{Type: 3, Value: &r}
	return e
}

func DimShapeOf(e *Element) string {
	if e == nil {
		return ""
	}
	return e.Shape
}

// dimContentBounds ports the shape-specific text insets from canvas-harness.
func dimContentBounds(e *Element) Box {
	b := Box{e.X, e.Y, e.W, e.H}
	switch e.Shape {
	case "tag":
		x := math.Min(e.H*.5, e.W*.3)
		b.X += x
		b.W -= x
	case "capsule":
		x := math.Min(e.H*.55, math.Min(e.W*.28, 56)) * .85
		b.X += x
		b.W -= x
	case "thought-cloud":
		y := math.Min(e.H*.45, math.Min(e.W*.4, e.H*1.2)) * .55
		b.Y += y
		b.H -= y
	case "layered-diamond", "soft-diamond":
		b.W = e.W / math.Sqrt2
		b.H = e.H / math.Sqrt2
		b.X += (e.W - b.W) / 2
		b.Y += (e.H - b.H) / 2
	case "layered-ellipse":
		b.W = e.W * .7
		b.H = e.H * .7
		b.X += e.W * .15
		b.Y += e.H * .15
	}
	if e.Scale[0] < 0 {
		b.X = e.X + e.W - (b.X - e.X) - b.W
	}
	if e.Scale[1] < 0 {
		b.Y = e.Y + e.H - (b.Y - e.Y) - b.H
	}
	return b
}

func dimShapePath(kind string, x, y, w, h, radius float64) string {
	var b strings.Builder
	put := func(op string, values ...float64) {
		b.WriteString(op)
		for _, v := range values {
			fmt.Fprintf(&b, " %g", v)
		}
		b.WriteByte(' ')
	}
	r := math.Max(0, math.Min(radius, math.Min(w/2, h/2)))
	switch kind {
	case "ellipse":
		put("M", x+w/2, y)
		put("A", w/2, h/2, 0, 1, 0, x+w/2, y+h)
		put("A", w/2, h/2, 0, 1, 0, x+w/2, y)
	case "diamond":
		ps := []Point{{x + w/2, y}, {x + w, y + h/2}, {x + w/2, y + h}, {x, y + h/2}}
		s := math.Min(radius*math.Sqrt2, math.Max(0, Distance(ps[0], ps[1])/2-.01))
		if s <= .0001 {
			for i, p := range ps {
				op := "L"
				if i == 0 {
					op = "M"
				}
				put(op, p[0], p[1])
			}
		} else {
			at := Add(ps[0], ScalePoint(unit(Sub(ps[1], ps[0])), s))
			put("M", at[0], at[1])
			for i := 1; i <= 4; i++ {
				p, prev, next := ps[i%4], ps[(i+3)%4], ps[(i+1)%4]
				a := Add(p, ScalePoint(unit(Sub(prev, p)), s))
				z := Add(p, ScalePoint(unit(Sub(next, p)), s))
				put("L", a[0], a[1])
				put("Q", p[0], p[1], z[0], z[1])
			}
		}
	case "thought-cloud":
		dw := math.Min(w*.4, h*1.2)
		dh := math.Min(h*.45, dw)
		dx := math.Max(0, math.Min(w-dw, w*.3-dw/2))
		cx, cy, rx, ry := x+dx+dw/2, y+dh/2, dw/2, dh/2
		by := y + dh*.55
		r = math.Max(0, math.Min(radius, math.Min((y+h-by)/2, w/2)))
		xl, xr := x+dx, x+dx+dw
		if ry > 0 {
			t := (by - cy) / ry
			if math.Abs(t) < 1 {
				offset := rx * math.Sqrt(1-t*t)
				xl, xr = cx-offset, cx+offset
			}
		}
		xl, xr = math.Max(x+r, xl), math.Min(x+w-r, xr)
		put("M", x+r, by)
		put("L", xl, by)
		put("A", rx, ry, 0, 1, 1, xr, by)
		put("L", x+w-r, by)
		put("Q", x+w, by, x+w, by+r)
		put("L", x+w, y+h-r)
		put("Q", x+w, y+h, x+w-r, y+h)
		put("L", x+r, y+h)
		put("Q", x, y+h, x, y+h-r)
		put("L", x, by+r)
		put("Q", x, by, x+r, by)
	case "tag":
		notch := math.Min(h*.5, w*.3)
		left := x + math.Max(0, math.Min(notch, w))
		right, bottom := x+w, y+h
		if left-x <= .001 {
			return dimShapePath("rect", x, y, w, h, radius)
		}
		rb := math.Min(radius, math.Min(h/2, (right-left)/2))
		rj := math.Min(radius, math.Min(h*.45, (left-x)*.8))
		tip := Point{x, y + h/2}
		top, bot := Point{left, y + rj}, Point{left, bottom - rj}
		dt, db := Sub(tip, top), Sub(tip, bot)
		lt, lb := math.Max(Distance(tip, top), 1e-30), math.Max(Distance(tip, bot), 1e-30)
		t := math.Max(0, math.Min(6, math.Min(lt, lb)*.49))
		enter, exit := Sub(tip, ScalePoint(db, t/lb)), Sub(tip, ScalePoint(dt, t/lt))
		k := rj * .65
		put("M", left+rb, y)
		put("L", right-rb, y)
		put("Q", right, y, right, y+rb)
		put("L", right, bottom-rb)
		put("Q", right, bottom, right-rb, bottom)
		put("L", left+rb, bottom)
		put("C", left+rb-k, bottom, bot[0]-db[0]/lb*k, bot[1]-db[1]/lb*k, bot[0], bot[1])
		put("L", enter[0], enter[1])
		if t > 0 {
			put("Q", tip[0], tip[1], exit[0], exit[1])
		}
		put("L", top[0], top[1])
		put("C", top[0]-dt[0]/lt*k, top[1]-dt[1]/lt*k, left+rb-k, y, left+rb, y)
	default:
		if r == 0 {
			put("M", x, y)
			put("L", x+w, y)
			put("L", x+w, y+h)
			put("L", x, y+h)
		} else {
			put("M", x+r, y)
			put("L", x+w-r, y)
			put("Q", x+w, y, x+w, y+r)
			put("L", x+w, y+h-r)
			put("Q", x+w, y+h, x+w-r, y+h)
			put("L", x+r, y+h)
			put("Q", x, y+h, x, y+h-r)
			put("L", x, y+r)
			put("Q", x, y, x+r, y)
		}
	}
	put("Z")
	return b.String()
}

type dimPart struct {
	kind       string
	x, y, w, h float64
	back       bool
}

func dimParts(e *Element) []dimPart {
	w, h := e.W, e.H
	switch e.Shape {
	case "capsule":
		d := math.Min(h*.55, math.Min(w*.28, 56))
		x := d * .85
		return []dimPart{{"ellipse", 0, (h - d) / 2, d, d, false}, {"rect", x, 0, w - x, h, false}}
	case "layered-rect", "layered-ellipse", "layered-diamond":
		kind := strings.TrimPrefix(e.Shape, "layered-")
		off := math.Min(12, math.Min(w*.15, h*.15))
		return []dimPart{{kind, off, off, w, h, true}, {kind, 0, 0, w, h, false}}
	case "soft-diamond":
		return []dimPart{{"diamond", -w * .04, -h * .04, w * 1.08, h * 1.08, true}, {"diamond", w * .02, h * .02, w * .96, h * .96, false}}
	}
	return []dimPart{{e.Shape, 0, 0, w, h, false}}
}
func dimDarken(color string) string {
	v, err := strconv.ParseUint(strings.TrimPrefix(color, "#"), 16, 32)
	if err != nil || len(color) != 7 {
		return color
	}
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(float64(v>>16&255)*.8)), int(math.Round(float64(v>>8&255)*.8)), int(math.Round(float64(v&255)*.8)))
}
func dimShapePrimitives(e *Element) []Primitive {
	if e.W <= 0 || e.H <= 0 {
		return nil
	}
	var out []Primitive
	for i, part := range dimParts(e) {
		o := RoughOptions(e)
		o.Seed += int64(i * 7919)
		o.DisableMultiStroke = true
		o.PreserveVertices = true
		o.Bowing = 2
		o.CurveStepCount = 5
		o.MaxRandomnessOffset = 1.3
		if part.back {
			o.Fill = dimDarken(o.Fill)
			o.Stroke = dimDarken(o.Stroke)
		}
		r := float64(12)
		if e.Roundness == nil {
			r = 0
		} else if e.Roundness.Value != nil {
			r = *e.Roundness.Value
		}
		path := dimShapePath(part.kind, part.x, part.y, part.w, part.h, r)
		// dim0 paints each fill then its outline, including composite layers.
		fill := o
		fill.Stroke = ""
		fill.Roughness = 0
		fill.FillStyle = "solid"
		fx, fy := part.x, part.y
		if e.Roughness > 0 {
			fx -= 3
			fy -= 2
		}
		out = append(out, primitives(rough.Path(dimShapePath(part.kind, fx, fy, part.w, part.h, r), fill), e.Opacity/100)...)
		o.Fill = ""
		out = append(out, primitives(rough.Path(path, o), e.Opacity/100)...)
	}
	if e.Scale[0] < 0 || e.Scale[1] < 0 {
		for _, p := range out {
			for _, op := range p.Ops {
				for j := 0; j+1 < len(op.Data); j += 2 {
					if e.Scale[0] < 0 {
						op.Data[j] = e.W - op.Data[j]
					}
					if e.Scale[1] < 0 {
						op.Data[j+1] = e.H - op.Data[j+1]
					}
				}
			}
		}
	}
	return out
}
