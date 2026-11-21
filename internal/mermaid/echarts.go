package mermaid

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseECharts lays out an ECharts option written as strict JSON, as an
// ```echarts fence holds it (Vditor, Arya): a title and subtitle, a legend,
// and pie, bar, line or scatter series on category and value axes. Colors
// come from the option (color, itemStyle, backgroundColor, textStyle) or
// ECharts' default palette; tooltips and animation have nothing to draw.
func ParseECharts(src string, measure MeasureFunc) (Diagram, error) {
	if measure == nil {
		measure = defaultMeasure
	}
	var opt map[string]any
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	if err := dec.Decode(&opt); err != nil {
		return Diagram{}, fmt.Errorf("ECharts option is not valid JSON: %v", err)
	}
	c := &chart{opt: opt, measure: measure, d: Diagram{Kind: "chart", W: 640, H: 400}}
	if p := jsonList(opt["color"]); len(p) > 0 {
		for _, v := range p {
			if col, ok := cssColor(jsonString(v)); ok {
				c.palette = append(c.palette, col)
			}
		}
	}
	if len(c.palette) == 0 {
		c.palette = chartPalette
	}
	if bg, ok := cssColor(jsonString(opt["backgroundColor"])); ok {
		c.bg = bg
		c.d.Shapes = append(c.d.Shapes, ShapeOp{Kind: ShapeRect, Role: RoleChartBack, W: c.d.W, H: c.d.H, Radius: 6, Fill: bg})
		// Text the option leaves uncolored stays readable on its background.
		c.text = [4]uint8{0x33, 0x33, 0x33, 255}
		if luminance(bg) < 0.5 {
			c.text = [4]uint8{0xee, 0xee, 0xee, 255}
		}
	}
	series := jsonList(opt["series"])
	if len(series) == 0 {
		if m, ok := opt["series"].(map[string]any); ok {
			series = []any{m}
		}
	}
	if len(series) == 0 {
		return Diagram{}, errors.New("ECharts option has no series")
	}
	c.title()
	c.legend(series)
	var axisSeries []map[string]any
	for _, raw := range series {
		s, _ := raw.(map[string]any)
		if s == nil {
			continue
		}
		switch t := jsonString(s["type"]); t {
		case "pie":
			c.pie(s)
		case "bar", "line", "scatter":
			axisSeries = append(axisSeries, s)
		default:
			return Diagram{}, fmt.Errorf("ECharts series type %q is not supported", t)
		}
	}
	if len(axisSeries) > 0 {
		if err := c.axes(axisSeries); err != nil {
			return Diagram{}, err
		}
	}
	return c.d, nil
}

type chart struct {
	opt     map[string]any
	measure MeasureFunc
	d       Diagram
	palette [][4]uint8
	bg      [4]uint8
	text    [4]uint8
	// top and left are where the title and a left legend end; plot room
	// starts below and right of them.
	top, left float32
	colorAt   int
}

func (c *chart) nextColor() [4]uint8 {
	col := c.palette[c.colorAt%len(c.palette)]
	c.colorAt++
	return col
}

// label adds text at x (aligned by align: -1 left, 0 center, 1 right of x)
// with its top at y, and returns its size.
func (c *chart) label(text string, x, y, size float32, bold bool, align int, color [4]uint8) (w, h float32) {
	w, h = c.measure(text, size, bold)
	switch align {
	case 0:
		x -= w / 2
	case 1:
		x -= w
	}
	if color[3] == 0 {
		color = c.text
	}
	c.d.Labels = append(c.d.Labels, LabelOp{Text: text, X: x, Y: y, W: w, H: h, Size: size, Bold: bold, Align: -1, Color: color})
	return w, h
}

func (c *chart) title() {
	t, _ := c.opt["title"].(map[string]any)
	if t == nil {
		c.top = 12
		return
	}
	text, sub := jsonString(t["text"]), jsonString(t["subtext"])
	pos := jsonString(t["left"])
	if pos == "" {
		pos = jsonString(t["x"])
	}
	x, align := float32(12), -1
	switch pos {
	case "center":
		x, align = c.d.W/2, 0
	case "right":
		x, align = c.d.W-12, 1
	}
	y := float32(10)
	if text != "" {
		_, h := c.label(text, x, y, 18, true, align, styleColor(t, "textStyle"))
		y += h + 4
	}
	if sub != "" {
		_, h := c.label(sub, x, y, 12, false, align, styleColor(t, "subtextStyle"))
		y += h + 4
	}
	c.top = y + 8
}

// legend lists the series (or a pie's data) names, by default across the
// top under the title, or down the left with orient vertical.
func (c *chart) legend(series []any) {
	l, _ := c.opt["legend"].(map[string]any)
	if l == nil || !jsonBool(l["show"], true) {
		return
	}
	var names []string
	for _, v := range jsonList(l["data"]) {
		if n := jsonString(v); n != "" {
			names = append(names, n)
		} else if m, ok := v.(map[string]any); ok {
			names = append(names, jsonString(m["name"]))
		}
	}
	if len(names) == 0 {
		for _, raw := range series {
			s, _ := raw.(map[string]any)
			if jsonString(s["type"]) == "pie" {
				for _, dv := range jsonList(s["data"]) {
					if m, ok := dv.(map[string]any); ok {
						names = append(names, jsonString(m["name"]))
					}
				}
			} else if n := jsonString(s["name"]); n != "" {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		return
	}
	color := styleColor(l, "textStyle")
	vertical := jsonString(l["orient"]) == "vertical"
	pos := jsonString(l["left"])
	if pos == "" {
		pos = jsonString(l["x"])
	}
	colorOf := c.legendColors(series, names)
	if vertical {
		x := float32(12)
		if pos == "right" {
			x = c.d.W - 120
		}
		y := c.top
		maxW := float32(0)
		for i, n := range names {
			c.d.Shapes = append(c.d.Shapes, ShapeOp{Kind: ShapeRect, Role: RoleChart, X: x, Y: y + 2, W: 25, H: 14, Radius: 3, Fill: colorOf[i]})
			w, h := c.label(n, x+31, y+9-6, 12, false, -1, color)
			maxW = max(maxW, w)
			y += max(h, 16) + 8
		}
		if pos != "right" {
			c.left = x + 31 + maxW + 16
		}
		return
	}
	total := float32(0)
	widths := make([]float32, len(names))
	for i, n := range names {
		w, _ := c.measure(n, 12, false)
		widths[i] = 25 + 5 + w
		total += widths[i] + 10
	}
	x := (c.d.W - total + 10) / 2
	y := c.top
	for i, n := range names {
		c.d.Shapes = append(c.d.Shapes, ShapeOp{Kind: ShapeRect, Role: RoleChart, X: x, Y: y + 2, W: 25, H: 14, Radius: 3, Fill: colorOf[i]})
		c.label(n, x+30, y+3, 12, false, -1, color)
		x += widths[i] + 10
	}
	c.top = y + 28
}

// legendColors gives each legend name its series' or slice's color.
func (c *chart) legendColors(series []any, names []string) [][4]uint8 {
	out := make([][4]uint8, len(names))
	byName := map[string][4]uint8{}
	i := 0
	for _, raw := range series {
		s, _ := raw.(map[string]any)
		if jsonString(s["type"]) == "pie" {
			for k, dv := range jsonList(s["data"]) {
				m, _ := dv.(map[string]any)
				col := c.palette[k%len(c.palette)]
				if v, ok := cssColor(itemColor(m)); ok {
					col = v
				}
				byName[jsonString(m["name"])] = col
			}
			continue
		}
		col := c.palette[i%len(c.palette)]
		if v, ok := cssColor(itemColor(s)); ok {
			col = v
		}
		byName[jsonString(s["name"])] = col
		i++
	}
	for k, n := range names {
		out[k] = byName[n]
		if out[k][3] == 0 {
			out[k] = c.palette[k%len(c.palette)]
		}
	}
	return out
}

func (c *chart) pie(s map[string]any) {
	var items []pieSlice
	total := 0.0
	for k, dv := range jsonList(s["data"]) {
		var name string
		var v float64
		col := c.palette[k%len(c.palette)]
		switch m := dv.(type) {
		case map[string]any:
			name, v = jsonString(m["name"]), jsonFloat(m["value"])
			if cc, ok := cssColor(itemColor(m)); ok {
				col = cc
			}
		default:
			v = jsonFloat(m)
		}
		if v < 0 || math.IsNaN(v) {
			continue
		}
		items = append(items, pieSlice{name: name, value: v, color: col})
		total += v
	}
	w, h := c.d.W, c.d.H
	cx, cy := percent(jsonList(s["center"]), 0, w, w/2), percent(jsonList(s["center"]), 1, h, h/2)
	half := min(w, h) / 2
	inner, outer := float32(0), half*0.75
	switch r := s["radius"].(type) {
	case []any:
		if len(r) >= 2 {
			inner, outer = percentValue(r[0], half), percentValue(r[1], half)
		}
	case nil:
	default:
		outer = percentValue(r, half)
	}
	// The plot keeps clear of the title and a left legend.
	if room := cy - c.top; room < outer+12 {
		cy = c.top + outer + 12
	}
	if c.left > 0 && cx-outer-90 < c.left {
		cx = c.left + outer + 90
	}
	showLabel := true
	if l, ok := s["label"].(map[string]any); ok {
		showLabel = jsonBool(l["show"], true)
	}
	if !showLabel {
		for i := range items {
			items[i].name = ""
		}
	}
	drawPie(&c.d, items, cx, cy, inner, outer, total, c.measure, c.text, false)
	c.fit()
}

// axes draws bar, line and scatter series on a category axis and a value
// axis; a category yAxis turns the bars sideways.
func (c *chart) axes(series []map[string]any) error {
	xa, ya := firstAxis(c.opt["xAxis"]), firstAxis(c.opt["yAxis"])
	horizontal := jsonString(ya["type"]) == "category"
	cat := xa
	if horizontal {
		cat = ya
	}
	var cats []string
	for _, v := range jsonList(cat["data"]) {
		if m, ok := v.(map[string]any); ok {
			cats = append(cats, jsonString(m["value"]))
		} else {
			cats = append(cats, jsonText(v))
		}
	}
	values := make([][]float64, len(series))
	lo, hi := 0.0, 0.0
	for i, s := range series {
		for _, v := range jsonList(s["data"]) {
			f := jsonFloat(v)
			if m, ok := v.(map[string]any); ok {
				f = jsonFloat(m["value"])
			}
			if arr, ok := v.([]any); ok && len(arr) >= 2 {
				f = jsonFloat(arr[1])
			}
			values[i] = append(values[i], f)
			if !math.IsNaN(f) {
				lo, hi = min(lo, f), max(hi, f)
			}
		}
		for len(cats) < len(values[i]) {
			cats = append(cats, strconv.Itoa(len(cats)+1))
		}
	}
	if len(cats) == 0 {
		return errors.New("ECharts axis series have no data")
	}
	step, ticks := niceTicks(lo, hi)
	vlo, vhi := ticks[0], ticks[len(ticks)-1]
	// The grid: ECharts' default margins, below the title and legend.
	left, right := max(c.left, c.d.W*0.1), c.d.W*0.9
	top, bottom := max(c.top, 60), c.d.H-60
	if horizontal {
		w := float32(0)
		for _, s := range cats {
			lw, _ := c.measure(s, 12, false)
			w = max(w, lw)
		}
		left = max(left, 16+w+8)
	}
	valueAt := func(v float64) float32 {
		f := float32((v - vlo) / (vhi - vlo))
		if horizontal {
			return left + (right-left)*f
		}
		return bottom - (bottom-top)*f
	}
	band := (right - left) / float32(len(cats))
	if horizontal {
		band = (bottom - top) / float32(len(cats))
	}
	catAt := func(i int) float32 {
		if horizontal {
			return top + band*(float32(i)+0.5)
		}
		return left + band*(float32(i)+0.5)
	}
	// Split lines and value labels.
	for _, tv := range ticks {
		p := valueAt(tv)
		text := strconv.FormatFloat(tv, 'f', -1, 64)
		if step < 1 {
			text = strconv.FormatFloat(tv, 'f', 2, 64)
		}
		if horizontal {
			c.d.Paths = append(c.d.Paths, PathOp{Points: [][2]float32{{p, top}, {p, bottom}}, Role: RoleGrid})
			c.label(text, p, bottom+8, 12, false, 0, styleColor(axisLabelStyle(xa), ""))
		} else {
			c.d.Paths = append(c.d.Paths, PathOp{Points: [][2]float32{{left, p}, {right, p}}, Role: RoleGrid})
			_, h := c.measure(text, 12, false)
			c.label(text, left-8, p-h/2, 12, false, 1, styleColor(axisLabelStyle(ya), ""))
		}
	}
	// The category axis line and labels.
	if horizontal {
		c.d.Paths = append(c.d.Paths, PathOp{Points: [][2]float32{{left, top}, {left, bottom}}, Role: RoleGrid})
	} else {
		c.d.Paths = append(c.d.Paths, PathOp{Points: [][2]float32{{left, bottom}, {right, bottom}}, Role: RoleGrid})
	}
	for i, name := range cats {
		p := catAt(i)
		if horizontal {
			_, h := c.measure(name, 12, false)
			c.label(name, left-8, p-h/2, 12, false, 1, styleColor(axisLabelStyle(ya), ""))
		} else {
			c.label(name, p, bottom+8, 12, false, 0, styleColor(axisLabelStyle(xa), ""))
		}
	}
	bars := 0
	for _, s := range series {
		if jsonString(s["type"]) == "bar" {
			bars++
		}
	}
	barW := band * 0.6 / float32(max(1, bars))
	barIdx := 0
	zero := valueAt(max(vlo, min(0, vhi)))
	for i, s := range series {
		col := c.nextColor()
		if v, ok := cssColor(itemColor(s)); ok {
			col = v
		}
		switch jsonString(s["type"]) {
		case "bar":
			off := -band*0.3 + barW*float32(barIdx)
			barIdx++
			for k, v := range values[i] {
				if math.IsNaN(v) {
					continue
				}
				p, q := catAt(k)+off, valueAt(v)
				r := ShapeOp{Kind: ShapeRect, Role: RoleChart, Fill: col, Radius: 2}
				if horizontal {
					r.X, r.Y, r.W, r.H = min(zero, q), p, abs32(q-zero), barW*0.92
				} else {
					r.X, r.Y, r.W, r.H = p, min(zero, q), barW*0.92, abs32(q-zero)
				}
				c.d.Shapes = append(c.d.Shapes, r)
			}
		case "line", "scatter":
			var pts [][2]float32
			for k, v := range values[i] {
				if math.IsNaN(v) {
					continue
				}
				pt := [2]float32{catAt(k), valueAt(v)}
				if horizontal {
					pt = [2]float32{valueAt(v), catAt(k)}
				}
				pts = append(pts, pt)
			}
			if jsonString(s["type"]) == "line" {
				if _, area := s["areaStyle"]; area && len(pts) > 1 && !horizontal {
					poly := append([][2]float32{{pts[0][0], zero}}, pts...)
					poly = append(poly, [2]float32{pts[len(pts)-1][0], zero})
					fill := col
					fill[3] = 110
					c.d.Shapes = append(c.d.Shapes, ShapeOp{Kind: ShapePolygon, Role: RoleChart, X: left, Y: top, W: right - left, H: bottom - top, Fill: fill, Points: poly})
				}
				if len(pts) > 1 {
					c.d.Paths = append(c.d.Paths, PathOp{Points: pts, Color: col, Width: 2})
				}
			}
			r := float32(3)
			if jsonString(s["type"]) == "scatter" {
				r = 5
			}
			for _, pt := range pts {
				c.d.Shapes = append(c.d.Shapes, ShapeOp{Kind: ShapeCircle, Role: RoleChart, X: pt[0] - r, Y: pt[1] - r, W: 2 * r, H: 2 * r, Fill: col, Stroke: [4]uint8{255, 255, 255, 255}})
			}
		}
	}
	c.fit()
	return nil
}

// fit grows the drawing to hold every label.
func (c *chart) fit() {
	for _, lb := range c.d.Labels {
		c.d.W = max(c.d.W, lb.X+lb.W+8)
		c.d.H = max(c.d.H, lb.Y+lb.H+8)
	}
	for i := range c.d.Shapes {
		if c.d.Shapes[i].Role == RoleChartBack {
			c.d.Shapes[i].W, c.d.Shapes[i].H = c.d.W, c.d.H
		}
	}
}

// niceTicks are about five round values from lo to hi, and their step.
func niceTicks(lo, hi float64) (float64, []float64) {
	if hi <= lo {
		hi = lo + 1
	}
	raw := (hi - lo) / 5
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	step := mag
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		step = m * mag
		if step >= raw {
			break
		}
	}
	start := math.Floor(lo/step) * step
	var out []float64
	for v := start; v <= hi+step*0.5; v += step {
		out = append(out, math.Round(v/step)*step)
		if v >= hi {
			break
		}
	}
	if len(out) < 2 {
		out = append(out, out[0]+step)
	}
	return step, out
}

func firstAxis(v any) map[string]any {
	switch a := v.(type) {
	case map[string]any:
		return a
	case []any:
		if len(a) > 0 {
			m, _ := a[0].(map[string]any)
			return m
		}
	}
	return nil
}

func axisLabelStyle(axis map[string]any) map[string]any {
	l, _ := axis["axisLabel"].(map[string]any)
	return l
}

// styleColor is the color of m[key].color, or of m.color when key is "".
func styleColor(m map[string]any, key string) [4]uint8 {
	if m == nil {
		return [4]uint8{}
	}
	if key != "" {
		m, _ = m[key].(map[string]any)
	}
	col, _ := cssColor(jsonString(m["color"]))
	return col
}

func itemColor(m map[string]any) string {
	if m == nil {
		return ""
	}
	if is, ok := m["itemStyle"].(map[string]any); ok {
		if c := jsonString(is["color"]); c != "" {
			return c
		}
		if n, ok := is["normal"].(map[string]any); ok {
			return jsonString(n["color"])
		}
	}
	return jsonString(m["color"])
}

// percent reads list[i] as a percentage of whole ("50%") or a number,
// else def.
func percent(list []any, i int, whole, def float32) float32 {
	if i >= len(list) {
		return def
	}
	return percentValue(list[i], whole)
}

func percentValue(v any, whole float32) float32 {
	if s := jsonString(v); strings.HasSuffix(s, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err == nil {
			return whole * float32(f) / 100
		}
	}
	return float32(jsonFloat(v))
}

func jsonList(v any) []any {
	l, _ := v.([]any)
	return l
}

func jsonString(v any) string {
	s, _ := v.(string)
	return s
}

func jsonText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	}
	return fmt.Sprint(v)
}

func jsonFloat(v any) float64 {
	switch t := v.(type) {
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f
		}
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
	case float64:
		return t
	}
	return math.NaN()
}

func jsonBool(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func luminance(c [4]uint8) float64 {
	return (0.299*float64(c[0]) + 0.587*float64(c[1]) + 0.114*float64(c[2])) / 255
}

// cssColor reads #rgb, #rrggbb, #rrggbbaa, rgb() and rgba() colors.
func cssColor(s string) ([4]uint8, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	hex := func(h string) (uint8, bool) {
		v, err := strconv.ParseUint(h, 16, 8)
		return uint8(v), err == nil
	}
	switch {
	case strings.HasPrefix(s, "#") && (len(s) == 4 || len(s) == 7 || len(s) == 9):
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		r, ok1 := hex(h[0:2])
		g, ok2 := hex(h[2:4])
		b, ok3 := hex(h[4:6])
		a := uint8(255)
		ok4 := true
		if len(h) == 8 {
			a, ok4 = hex(h[6:8])
		}
		return [4]uint8{r, g, b, a}, ok1 && ok2 && ok3 && ok4
	case strings.HasPrefix(s, "rgb"):
		open, end := strings.IndexByte(s, '('), strings.IndexByte(s, ')')
		if open < 0 || end < open {
			return [4]uint8{}, false
		}
		parts := strings.Split(s[open+1:end], ",")
		if len(parts) < 3 {
			return [4]uint8{}, false
		}
		var out [4]uint8
		out[3] = 255
		for i, p := range parts[:min(4, len(parts))] {
			f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil {
				return [4]uint8{}, false
			}
			if i == 3 {
				f *= 255
			}
			out[i] = uint8(max(0, min(255, math.Round(f))))
		}
		return out, true
	}
	if col, ok := namedChartColors[s]; ok {
		return col, true
	}
	return [4]uint8{}, false
}

var namedChartColors = map[string][4]uint8{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255}, "red": {255, 0, 0, 255},
	"green": {0, 128, 0, 255}, "blue": {0, 0, 255, 255}, "gray": {128, 128, 128, 255},
	"grey": {128, 128, 128, 255}, "orange": {255, 165, 0, 255}, "yellow": {255, 255, 0, 255},
	"purple": {128, 0, 128, 255},
}
