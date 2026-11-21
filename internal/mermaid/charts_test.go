package mermaid

import (
	"strings"
	"testing"
)

// Mermaid's gantt reads dates, durations, "after" and implicit starts, and
// draws one bar per task within its axis.
func TestGanttResolvesTasksAndDrawsBars(t *testing.T) {
	src := `gantt
  title 项目开发流程
  dateFormat YYYY-MM-DD
  section 项目确定
    需求分析       :a1, 2019-06-22, 3d
    可行性报告     :after a1, 5d
    概念验证       : 5d
  section 项目实施
    概要设计      :2019-07-05  , 5d
    里程碑        :milestone, m1, 2019-07-10, 0d
    测试          :crit, done, 2019-07-22, 2019-07-27`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagramBounds(t, d)
	var bars []ShapeOp
	for _, s := range d.Shapes {
		if s.Role == RoleTask {
			bars = append(bars, s)
		}
	}
	if len(bars) != 6 {
		t.Fatalf("got %d bars, want 6", len(bars))
	}
	// 需求分析 ends where 可行性报告 starts, which ends where 概念验证 starts.
	if abs32(bars[0].X+bars[0].W-bars[1].X) > 0.5 || abs32(bars[1].X+bars[1].W-bars[2].X) > 0.5 {
		t.Fatalf("after/implicit starts not chained: %+v", bars[:3])
	}
	if bars[4].Kind != ShapeDiamond || bars[4].Tone != ToneMilestone {
		t.Fatalf("milestone: %+v", bars[4])
	}
	if bars[5].Tone != ToneDone {
		t.Fatalf("tags: %+v", bars[5])
	}
	if _, err := ParseAndLayout("gantt\n  task :after missing, 1d", nil); err == nil {
		t.Fatal("an unknown after task should fail")
	}
}

func TestMermaidPieAndEChartsCharts(t *testing.T) {
	pie, err := ParseAndLayout("pie showData\n  title Pets\n  \"Dogs\" : 386\n  \"Cats\" : 85", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagramBounds(t, pie)
	slices := 0
	for _, s := range pie.Shapes {
		if s.Kind == ShapePolygon {
			slices++
		}
	}
	if slices != 2 || pie.Kind != "pie" {
		t.Fatalf("pie: %d slices, kind %q", slices, pie.Kind)
	}

	echart, err := ParseECharts(`{"backgroundColor":"#212121","title":{"text":"访问来源","x":"center","textStyle":{"color":"#f2f2f2"}},
		"legend":{"orient":"vertical","left":"left","data":["搜索引擎","直接访问"]},
		"series":[{"type":"pie","radius":"55%","center":["50%","60%"],"data":[{"value":10440,"name":"搜索引擎","itemStyle":{"color":"#ef4136"}},{"value":4770,"name":"直接访问"}]}]}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagramBounds(t, echart)
	if echart.Shapes[0].Role != RoleChartBack || echart.Shapes[0].Fill != [4]uint8{0x21, 0x21, 0x21, 255} {
		t.Fatalf("background: %+v", echart.Shapes[0])
	}
	found := false
	for _, s := range echart.Shapes {
		if s.Kind == ShapePolygon && s.Fill == [4]uint8{0xef, 0x41, 0x36, 255} {
			found = true
		}
	}
	if !found {
		t.Fatal("itemStyle color not used")
	}

	bars, err := ParseECharts(`{"xAxis":{"type":"category","data":["Mon","Tue","Wed"]},"yAxis":{"type":"value"},
		"series":[{"name":"A","type":"bar","data":[120,200,150]},{"name":"B","type":"line","data":[80,90,70],"areaStyle":{}}]}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagramBounds(t, bars)
	rects := 0
	for _, s := range bars.Shapes {
		if s.Kind == ShapeRect && s.Role == RoleChart {
			rects++
		}
	}
	if rects != 3 {
		t.Fatalf("got %d bars, want 3", rects)
	}

	if _, err := ParseECharts(`{"series":[{"type":"sankey"}]}`, nil); err == nil || !strings.Contains(err.Error(), "sankey") {
		t.Fatalf("unsupported type error: %v", err)
	}
	if _, err := ParseECharts(`{series: []}`, nil); err == nil {
		t.Fatal("non-JSON option should fail")
	}
}

// A task name too wide for its bar shrinks to fit inside the bar rather than
// spilling beside it.
func TestGanttNamesShrinkIntoTheirBars(t *testing.T) {
	src := `gantt
  title 项目开发流程
  section 项目确定
    需求分析       :a1, 2019-06-22, 3d
    可行性报告     :after a1, 5d
    概念验证       : 5d
  section 项目实施
    概要设计      :2019-07-05  , 5d
    详细设计      :2019-07-08, 10d
    编码          :2019-07-15, 10d
    测试          :2019-07-22, 5d
  section 发布验收
    发布: 2d
    验收: 3d
    一个名字很长很长很长的任务: 1h`
	d, err := ParseAndLayout(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertDiagramBounds(t, d)
	var bars []ShapeOp
	for _, s := range d.Shapes {
		if s.Role == RoleTask {
			bars = append(bars, s)
		}
	}
	var names []LabelOp
	for _, lb := range d.Labels {
		if lb.Role == 0 && !lb.Bold {
			names = append(names, lb)
		}
	}
	if len(bars) != 10 || len(names) != len(bars) {
		t.Fatalf("got %d bars and %d names, want 10 each", len(bars), len(names))
	}
	for i, b := range bars {
		lb := names[i]
		if lb.X < b.X-0.01 || lb.X+lb.W > b.X+b.W+0.01 || lb.Y < b.Y-0.01 || lb.Y+lb.H > b.Y+b.H+0.01 {
			t.Fatalf("name %q (%+v) spills out of its bar %+v", lb.Text, lb, b)
		}
	}
	if names[0].Size >= 12 || names[9].Size >= names[0].Size || names[4].Size != 12 {
		t.Fatalf("only names that do not fit should shrink: %v, %v, %v", names[0].Size, names[9].Size, names[4].Size)
	}
}
