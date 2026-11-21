package mermaid

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// chartPalette is ECharts' default series palette, which Mermaid's pie
// colors follow too.
var chartPalette = [][4]uint8{
	{0x54, 0x70, 0xc6, 255}, {0x91, 0xcc, 0x75, 255}, {0xfa, 0xc8, 0x58, 255},
	{0xee, 0x66, 0x66, 255}, {0x73, 0xc0, 0xde, 255}, {0x3b, 0xa2, 0x72, 255},
	{0xfc, 0x84, 0x52, 255}, {0x9a, 0x60, 0xb4, 255}, {0xea, 0x7c, 0xcc, 255},
}

type pieSlice struct {
	name  string
	value float64
	color [4]uint8
}

// layoutMermaidPie reads Mermaid's pie: an optional showData, a title and
// "label" : value lines.
func layoutMermaidPie(lines []string, measure MeasureFunc) (Diagram, error) {
	showData := strings.Contains(strings.ToLower(lines[0]), "showdata")
	title := ""
	if f := strings.Fields(lines[0]); len(f) > 1 && strings.EqualFold(f[1], "title") {
		title = strings.TrimSpace(lines[0][strings.Index(strings.ToLower(lines[0]), "title")+len("title"):])
	}
	var parts []pieSlice
	for _, raw := range lines[1:] {
		low := strings.ToLower(raw)
		switch {
		case strings.HasPrefix(low, "title "):
			title = strings.TrimSpace(raw[len("title "):])
			continue
		case low == "showdata":
			showData = true
			continue
		case strings.HasPrefix(low, "acctitle") || strings.HasPrefix(low, "accdescr"):
			continue
		}
		colon := strings.LastIndexByte(raw, ':')
		if colon < 0 {
			continue
		}
		name := strings.Trim(strings.TrimSpace(raw[:colon]), `"'`)
		v, err := strconv.ParseFloat(strings.TrimSpace(raw[colon+1:]), 64)
		if err != nil || v < 0 {
			return Diagram{}, fmt.Errorf("pie slice %q needs a positive number", name)
		}
		parts = append(parts, pieSlice{name: name, value: v, color: chartPalette[len(parts)%len(chartPalette)]})
	}
	if len(parts) == 0 {
		return Diagram{}, errors.New("pie has no slices")
	}
	d := Diagram{Kind: "pie", W: 560, H: 360}
	top := float32(16)
	if title != "" {
		tw, th := measure(title, 15, true)
		d.Labels = append(d.Labels, LabelOp{Text: title, X: (d.W - tw) / 2, Y: 14, W: tw, H: th, Size: 15, Bold: true})
		top += th + 10
	}
	r := (d.H - top - 16) / 2
	cx, cy := 24+r, top+r
	total := 0.0
	for _, s := range parts {
		total += s.value
	}
	drawPie(&d, parts, cx, cy, 0, r, total, measure, [4]uint8{}, true)
	// Legend right of the pie.
	lx := cx + r + 36
	ly := cy - float32(len(parts))*22/2
	for i, s := range parts {
		text := s.name
		if showData {
			text += " [" + strconv.FormatFloat(s.value, 'f', -1, 64) + "]"
		}
		y := ly + float32(i)*22
		d.Shapes = append(d.Shapes, ShapeOp{Kind: ShapeRect, Role: RoleChart, X: lx, Y: y + 3, W: 14, H: 14, Radius: 3, Fill: s.color})
		w, h := measure(text, 12, false)
		d.Labels = append(d.Labels, LabelOp{Text: text, X: lx + 20, Y: y + 10 - h/2, W: w, H: h, Size: 12, Align: -1})
		d.W = max(d.W, lx+20+w+16)
	}
	return d, nil
}

// drawPie adds slices clockwise from twelve o'clock around cx, cy between
// radii inner and outer. Inside labels show percentages (Mermaid);
// otherwise names sit outside on leader lines (ECharts).
func drawPie(d *Diagram, items []pieSlice, cx, cy, inner, outer float32, total float64, measure MeasureFunc, textColor [4]uint8, inside bool) {
	if total <= 0 {
		return
	}
	type outLabel struct {
		text      string
		ax, ay    float32 // on the arc
		y, w, h   float32
		right     bool
		lineColor [4]uint8
	}
	var outs []outLabel
	angle := -math.Pi / 2
	for _, s := range items {
		sweep := 2 * math.Pi * s.value / total
		if sweep <= 0 {
			continue
		}
		var pts [][2]float32
		steps := max(2, int(math.Ceil(sweep/(math.Pi/90))))
		for k := 0; k <= steps; k++ {
			a := angle + sweep*float64(k)/float64(steps)
			pts = append(pts, [2]float32{cx + outer*float32(math.Cos(a)), cy + outer*float32(math.Sin(a))})
		}
		if inner > 0 {
			for k := steps; k >= 0; k-- {
				a := angle + sweep*float64(k)/float64(steps)
				pts = append(pts, [2]float32{cx + inner*float32(math.Cos(a)), cy + inner*float32(math.Sin(a))})
			}
		} else {
			pts = append(pts, [2]float32{cx, cy})
		}
		d.Shapes = append(d.Shapes, ShapeOp{Kind: ShapePolygon, Role: RoleChart, X: cx - outer, Y: cy - outer, W: 2 * outer, H: 2 * outer, Fill: s.color, Stroke: [4]uint8{255, 255, 255, 255}, Points: pts})
		mid := angle + sweep/2
		if inside {
			if sweep > 0.25 {
				text := strconv.FormatFloat(math.Round(s.value/total*1000)/10, 'f', -1, 64) + "%"
				w, h := measure(text, 12, true)
				rr := (inner + outer) / 2
				if inner == 0 {
					rr = outer * 0.62
				}
				x, y := cx+rr*float32(math.Cos(mid)), cy+rr*float32(math.Sin(mid))
				d.Labels = append(d.Labels, LabelOp{Text: text, X: x - w/2, Y: y - h/2, W: w, H: h, Size: 12, Bold: true, Color: [4]uint8{255, 255, 255, 255}})
			}
		} else if s.name != "" {
			w, h := measure(s.name, 12, false)
			outs = append(outs, outLabel{
				text: s.name, w: w, h: h,
				ax: cx + outer*float32(math.Cos(mid)), ay: cy + outer*float32(math.Sin(mid)),
				y:     cy + (outer+18)*float32(math.Sin(mid)),
				right: math.Cos(mid) >= 0, lineColor: s.color,
			})
		}
		angle += sweep
	}
	// Outside labels, kept apart on each side.
	for _, right := range []bool{true, false} {
		var side []int
		for i, o := range outs {
			if o.right == right {
				side = append(side, i)
			}
		}
		slices.SortFunc(side, func(a, b int) int {
			switch {
			case outs[a].y < outs[b].y:
				return -1
			case outs[a].y > outs[b].y:
				return 1
			}
			return 0
		})
		for k := 1; k < len(side); k++ {
			prev, cur := &outs[side[k-1]], &outs[side[k]]
			cur.y = max(cur.y, prev.y+prev.h+2)
		}
		for _, i := range side {
			o := outs[i]
			ex := cx + (outer+18)*sign32(o.right)
			if o.right {
				ex = max(ex, o.ax+10)
			} else {
				ex = min(ex, o.ax-10)
			}
			end := ex + 14*sign32(o.right)
			d.Paths = append(d.Paths, PathOp{Points: [][2]float32{{o.ax, o.ay}, {ex, o.y}, {end, o.y}}, Color: o.lineColor, Width: 1})
			lx := end + 4
			if !o.right {
				lx = end - 4 - o.w
			}
			d.Labels = append(d.Labels, LabelOp{Text: o.text, X: lx, Y: o.y - o.h/2, W: o.w, H: o.h, Size: 12, Align: -1, Color: textColor})
		}
	}
}

func sign32(right bool) float32 {
	if right {
		return 1
	}
	return -1
}
