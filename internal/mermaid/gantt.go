package mermaid

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// A Gantt chart, as Mermaid's gantt diagram writes it: a title, a date
// format, sections and tasks with tags (done, active, crit, milestone), an
// optional id, a start (a date or "after" other tasks, else the previous
// task's end) and an end (a date, a duration or "until" a task).

type ganttTask struct {
	name, id, section string
	tone              uint8
	milestone         bool
	start, end        time.Time
	// startSpec and endSpec are as written, resolved once every task is read.
	startSpec, endSpec string
	resolved           bool
}

func layoutGantt(lines []string, measure MeasureFunc) (Diagram, error) {
	var (
		title      string
		dateFormat = "YYYY-MM-DD"
		axisFormat = ""
		excludes   []string
		section    string
		tasks      []*ganttTask
	)
	for _, raw := range lines[1:] {
		low := strings.ToLower(raw)
		word := low
		if i := strings.IndexAny(low, " \t"); i >= 0 {
			word = low[:i]
		}
		rest := strings.TrimSpace(raw[len(word):])
		switch word {
		case "title":
			title = rest
			continue
		case "dateformat":
			dateFormat = rest
			continue
		case "axisformat":
			axisFormat = rest
			continue
		case "excludes":
			for _, f := range strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' }) {
				excludes = append(excludes, strings.ToLower(f))
			}
			continue
		case "section":
			section = rest
			continue
		case "includes", "todaymarker", "tickinterval", "weekday", "inclusiveenddates", "topaxis", "displaymode", "acctitle", "accdescr", "click":
			continue
		}
		colon := strings.IndexByte(raw, ':')
		if colon < 0 {
			continue
		}
		t := &ganttTask{name: strings.TrimSpace(raw[:colon]), section: section}
		var parts []string
		for _, p := range strings.Split(raw[colon+1:], ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		for len(parts) > 0 {
			tag := strings.ToLower(parts[0])
			switch tag {
			case "done":
				t.tone = ToneDone
			case "active":
				t.tone = ToneActive
			case "crit":
				t.tone = ToneCrit
			case "milestone":
				t.milestone = true
			default:
				tag = ""
			}
			if tag == "" {
				break
			}
			parts = parts[1:]
		}
		switch len(parts) {
		case 0:
			continue
		case 1:
			t.endSpec = parts[0]
		case 2:
			t.startSpec, t.endSpec = parts[0], parts[1]
		default:
			t.id, t.startSpec, t.endSpec = parts[0], parts[1], parts[2]
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return Diagram{}, errors.New("gantt has no tasks")
	}
	byID := map[string]*ganttTask{}
	for _, t := range tasks {
		if t.id != "" {
			byID[t.id] = t
		}
	}
	layout := ganttLayout(dateFormat)
	excluded := func(d time.Time) bool {
		for _, ex := range excludes {
			switch {
			case ex == "weekends" && (d.Weekday() == time.Saturday || d.Weekday() == time.Sunday):
				return true
			case strings.EqualFold(ex, strings.ToLower(d.Weekday().String())):
				return true
			}
			if day, err := parseGanttDate(ex, layout); err == nil && sameDay(day, d) {
				return true
			}
		}
		return false
	}
	// resolve settles a task's start and end, following "after" and "until"
	// to the tasks they name; a cycle or an unknown task is an error.
	var resolve func(i int, depth int) error
	resolve = func(i int, depth int) error {
		t := tasks[i]
		if t.resolved {
			return nil
		}
		if depth > len(tasks) {
			return fmt.Errorf("task %q depends on itself", t.name)
		}
		switch {
		case t.startSpec == "":
			if i == 0 {
				return fmt.Errorf("task %q has no start date", t.name)
			}
			if err := resolve(i-1, depth+1); err != nil {
				return err
			}
			t.start = tasks[i-1].end
		case strings.HasPrefix(strings.ToLower(t.startSpec), "after "):
			var latest time.Time
			for _, id := range strings.Fields(t.startSpec[len("after "):]) {
				dep := byID[id]
				if dep == nil {
					return fmt.Errorf("task %q comes after unknown task %q", t.name, id)
				}
				if err := resolveTask(tasks, dep, resolve, depth); err != nil {
					return err
				}
				if dep.end.After(latest) {
					latest = dep.end
				}
			}
			t.start = latest
		default:
			d, err := parseGanttDate(t.startSpec, layout)
			if err != nil {
				return fmt.Errorf("task %q: %v", t.name, err)
			}
			t.start = d
		}
		switch {
		case strings.HasPrefix(strings.ToLower(t.endSpec), "until "):
			var earliest time.Time
			for k, id := range strings.Fields(t.endSpec[len("until "):]) {
				dep := byID[id]
				if dep == nil {
					return fmt.Errorf("task %q runs until unknown task %q", t.name, id)
				}
				if err := resolveTask(tasks, dep, resolve, depth); err != nil {
					return err
				}
				if k == 0 || dep.start.Before(earliest) {
					earliest = dep.start
				}
			}
			t.end = earliest
		default:
			if dur, ok := parseGanttDuration(t.endSpec); ok {
				t.end = addGanttDuration(t.start, dur, t.endSpec, excluded)
			} else if d, err := parseGanttDate(t.endSpec, layout); err == nil {
				t.end = d
			} else {
				return fmt.Errorf("task %q: cannot read %q", t.name, t.endSpec)
			}
		}
		if t.milestone {
			t.end = t.start
		}
		if t.end.Before(t.start) {
			t.end = t.start
		}
		t.resolved = true
		return nil
	}
	for i := range tasks {
		if err := resolve(i, 0); err != nil {
			return Diagram{}, err
		}
	}

	lo, hi := tasks[0].start, tasks[0].end
	for _, t := range tasks {
		if t.start.Before(lo) {
			lo = t.start
		}
		if t.end.After(hi) {
			hi = t.end
		}
	}
	if !hi.After(lo) {
		hi = lo.Add(24 * time.Hour)
	}
	return drawGantt(title, tasks, lo, hi, axisFormat, measure), nil
}

// resolveTask resolves dep, which another task depends on, by its index.
func resolveTask(tasks []*ganttTask, dep *ganttTask, resolve func(int, int) error, depth int) error {
	for i, t := range tasks {
		if t == dep {
			return resolve(i, depth+1)
		}
	}
	return nil
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// ganttLayout turns a Mermaid (dayjs) date format into a Go layout.
func ganttLayout(format string) string {
	r := strings.NewReplacer("YYYY", "2006", "YY", "06", "MM", "01", "DD", "02", "HH", "15", "mm", "04", "ss", "05", "SSS", "000", "Do", "2", "M", "1", "D", "2", "H", "15")
	return r.Replace(strings.TrimSpace(format))
}

func parseGanttDate(s, layout string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if layout == "X" || layout == "x" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		if layout == "x" {
			return time.UnixMilli(n).UTC(), nil
		}
		return time.Unix(n, 0).UTC(), nil
	}
	for _, l := range []string{layout, "2006-01-02", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006/01/02", "15:04"} {
		if d, err := time.Parse(l, s); err == nil {
			return d, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot read date %q", s)
}

// parseGanttDuration reads a duration such as 3d, 1.5h, 2w or 30m.
func parseGanttDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		d      time.Duration
	}{{"ms", time.Millisecond}, {"s", time.Second}, {"m", time.Minute}, {"h", time.Hour}, {"d", 24 * time.Hour}, {"w", 7 * 24 * time.Hour}, {"M", 30 * 24 * time.Hour}, {"y", 365 * 24 * time.Hour}}
	for _, u := range units {
		if !strings.HasSuffix(s, u.suffix) {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-len(u.suffix)]), 64)
		if err != nil || n < 0 {
			return 0, false
		}
		return time.Duration(n * float64(u.d)), true
	}
	return 0, false
}

// addGanttDuration adds d to start; whole days skip the excluded ones.
func addGanttDuration(start time.Time, d time.Duration, spec string, excluded func(time.Time) bool) time.Time {
	if !strings.HasSuffix(spec, "d") && !strings.HasSuffix(spec, "w") {
		return start.Add(d)
	}
	days := int(math.Round(d.Hours() / 24))
	end := start
	for days > 0 {
		if !excluded(end) {
			days--
		}
		end = end.AddDate(0, 0, 1)
	}
	return end
}

// ganttStrftime formats t with the strftime directives Mermaid's
// axisFormat uses.
func ganttStrftime(format string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 == len(format) {
			b.WriteByte(format[i])
			continue
		}
		i++
		switch format[i] {
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		case 'y':
			fmt.Fprintf(&b, "%02d", t.Year()%100)
		case 'm':
			fmt.Fprintf(&b, "%02d", int(t.Month()))
		case 'd':
			fmt.Fprintf(&b, "%02d", t.Day())
		case 'e':
			b.WriteString(strconv.Itoa(t.Day()))
		case 'H':
			fmt.Fprintf(&b, "%02d", t.Hour())
		case 'M':
			fmt.Fprintf(&b, "%02d", t.Minute())
		case 'S':
			fmt.Fprintf(&b, "%02d", t.Second())
		case 'b':
			b.WriteString(t.Month().String()[:3])
		case 'B':
			b.WriteString(t.Month().String())
		case 'a':
			b.WriteString(t.Weekday().String()[:3])
		case 'A':
			b.WriteString(t.Weekday().String())
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}
	return b.String()
}

func drawGantt(title string, tasks []*ganttTask, lo, hi time.Time, axisFormat string, measure MeasureFunc) Diagram {
	const (
		pad      float32 = 16
		rowH     float32 = 28
		barH     float32 = 20
		labelPx  float32 = 12
		titlePx  float32 = 15
		axisPx   float32 = 11
		timeline float32 = 620
	)
	var d Diagram
	d.Kind = "gantt"
	top := pad
	if title != "" {
		tw, th := measure(title, titlePx, true)
		d.Labels = append(d.Labels, LabelOp{Text: title, W: tw, H: th, Size: titlePx, Bold: true})
		top += th + 12
	}
	// The section names take a column of their own left of the bars.
	secW := float32(0)
	for _, t := range tasks {
		if t.section != "" {
			w, _ := measure(t.section, labelPx, true)
			secW = max(secW, w)
		}
	}
	left := pad
	if secW > 0 {
		left += secW + 18
	}
	span := float32(hi.Sub(lo).Seconds())
	xOf := func(t time.Time) float32 { return left + timeline*float32(t.Sub(lo).Seconds())/span }
	width := left + timeline + pad

	// Section bands, alternating, with their names.
	rows := float32(len(tasks))
	for i := 0; i < len(tasks); {
		j := i
		for j < len(tasks) && tasks[j].section == tasks[i].section {
			j++
		}
		y := top + float32(i)*rowH
		h := float32(j-i) * rowH
		band := len(d.Shapes) % 2
		d.Shapes = append(d.Shapes, ShapeOp{Kind: ShapeRect, Role: RoleSection, X: pad / 2, Y: y, W: width - pad, H: h, Tone: uint8(band)})
		if name := tasks[i].section; name != "" {
			w, lh := measure(name, labelPx, true)
			d.Labels = append(d.Labels, LabelOp{Text: name, X: pad, Y: y + (h-lh)/2, W: w, H: lh, Size: labelPx, Bold: true, Align: -1, Role: RoleSubgraph})
		}
		i = j
	}
	chartBottom := top + rows*rowH

	// Ticks: a step of whole days, weeks or months that gives about eight.
	days := hi.Sub(lo).Hours() / 24
	step := 1
	for _, s := range []int{1, 2, 3, 5, 7, 14, 30, 61, 91, 182, 365} {
		step = s
		if days/float64(s) <= 9 {
			break
		}
	}
	if axisFormat == "" {
		axisFormat = "%Y-%m-%d"
		if days < 2 {
			axisFormat = "%H:%M"
		}
	}
	tick := time.Date(lo.Year(), lo.Month(), lo.Day(), 0, 0, 0, 0, lo.Location())
	if days < 2 {
		tick = lo
	}
	for !tick.After(hi) {
		if !tick.Before(lo) {
			x := xOf(tick)
			d.Paths = append(d.Paths, PathOp{Points: [][2]float32{{x, top}, {x, chartBottom}}, Role: RoleGrid})
			text := ganttStrftime(axisFormat, tick)
			w, h := measure(text, axisPx, false)
			d.Labels = append(d.Labels, LabelOp{Text: text, X: x - w/2, Y: chartBottom + 6, W: w, H: h, Size: axisPx, Role: RoleSubgraph})
		}
		if days < 2 {
			tick = tick.Add(time.Duration(max(1, int(days*24/8))) * time.Hour)
		} else {
			tick = tick.AddDate(0, 0, step)
		}
	}

	for i, t := range tasks {
		y := top + float32(i)*rowH + (rowH-barH)/2
		x0, x1 := xOf(t.start), xOf(t.end)
		lw, lh := measure(t.name, labelPx, false)
		if t.milestone {
			s := barH * 0.8
			d.Shapes = append(d.Shapes, ShapeOp{Kind: ShapeDiamond, Role: RoleTask, X: x0 - s/2, Y: y + (barH-s)/2, W: s, H: s, Tone: ToneMilestone})
			d.Labels = append(d.Labels, LabelOp{Text: t.name, X: x0 + s/2 + 6, Y: y + (barH-lh)/2, W: lw, H: lh, Size: labelPx, Align: -1})
			continue
		}
		w := max(2, x1-x0)
		d.Shapes = append(d.Shapes, ShapeOp{Kind: ShapeRect, Role: RoleTask, X: x0, Y: y, W: w, H: barH, Radius: 4, Tone: t.tone})
		// The name always stays inside its bar: a name too wide for the bar
		// shrinks until it fits, keeping a small inset when the bar has room
		// for one. Measuring is not exactly linear in size, so step down
		// until the measured width fits.
		size := labelPx
		inset := min(float32(6), w*0.1)
		if avail := w - 2*inset; lw > avail {
			size = labelPx * avail / lw
			lw, lh = measure(t.name, size, false)
			for lw > avail && size > 0.01 {
				size *= 0.95
				lw, lh = measure(t.name, size, false)
			}
		}
		d.Labels = append(d.Labels, LabelOp{Text: t.name, X: x0 + (w-lw)/2, Y: y + (barH-lh)/2, W: lw, H: lh, Size: size, Align: -1})
	}
	// Center the title over the timeline, and size the drawing.
	if title != "" {
		d.Labels[0].X = left + (timeline-d.Labels[0].W)/2
		d.Labels[0].Y = pad
	}
	d.W, d.H = width, chartBottom+axisPx+24
	for _, lb := range d.Labels {
		if lb.X < 0 {
			shift := -lb.X + pad
			for i := range d.Labels {
				d.Labels[i].X += shift
			}
			for i := range d.Shapes {
				d.Shapes[i].X += shift
			}
			for i := range d.Paths {
				for k := range d.Paths[i].Points {
					d.Paths[i].Points[k][0] += shift
				}
			}
			d.W += shift
			break
		}
	}
	for _, lb := range d.Labels {
		d.W = max(d.W, lb.X+lb.W+pad)
	}
	return d
}
