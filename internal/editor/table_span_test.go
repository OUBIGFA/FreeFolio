package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// wideTableMD is a table whose columns naturally exceed the prose column, as
// the comparison tables of research notes do.
func wideTableMD() string {
	head := "| 梯队 | 项目名称 | GitHub 仓库 | 开源协议 | 核心技术栈 | 核心定位与亮点 | 对 FreeFolio 的复用/借鉴定位 | 合规与风险提示 |"
	bar := "| --- | --- | --- | --- | --- | --- | --- | --- |"
	row := "| 一 | Tersa | vercel-labs/tersa | MIT | Next.js, React 19, React Flow, TipTap | 极简 AI 节点画布，支持文本/图像/多模型路由串联 | 高借鉴：极简节点分层（输入/生成/转换）与节点内联参数区 | 仅借鉴交互与模型，Web 代码不可直接复用 |"
	return "# 对比\n\n" + strings.Join([]string{head, bar, row, row, row}, "\n") + "\n\n结尾段落"
}

func newWideTester(md string, w int, opts Options) (*Editor, *ui.Tester) {
	e := New(md)
	tt := ui.NewTester(func(c *ui.Context) {
		o := opts
		o.Width, _ = c.Size()
		o.Theme = c.Theme()
		View(c, e, o)
	}, w, 900)
	tt.Frame()
	return e, tt
}

func firstTable(t *testing.T, e *Editor) decoration {
	t.Helper()
	for _, b := range e.Doc.Blocks {
		if b.Kind == "table" {
			if d, ok := e.tableDecoration(b.ID); ok {
				return d
			}
		}
	}
	t.Fatal("no laid out table")
	return decoration{}
}

func TestWideTableLeftCellsTakeTheCaret(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	focus(e, tt)
	d := firstTable(t, e)
	if d.rect.X >= e.padding-60 {
		t.Fatalf("table did not extend into the left margin: table x=%v padding=%v", d.rect.X, e.padding)
	}
	bounds, _ := tt.Find("文档")
	x, y := bounds.X+d.cols[0]+16, bounds.Y+d.rows[1]+12
	tt.ClickAt(x, y)
	tt.Frame()
	if len(e.blocks.ids) != 0 {
		t.Fatalf("a press on a widened table's first column selected blocks: %v", e.blocks.ids)
	}
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" || leaf.Owner != e.Doc.Blocks[1].ID {
		t.Fatalf("caret did not move into the clicked cell: %+v", leaf.Block.Kind)
	}
	if cur, ok := e.hoverCursor(0, d.cols[0]+16, d.rows[1]+12); ok && cur == ui.CursorDefault {
		t.Fatal("the widened table's first column shows the arrow instead of the I-beam")
	}
}

func TestResizingWideTableKeepsItsWidthAndColumnFloor(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	before := firstTable(t, e)
	if before.rect.W <= e.bodyWidth()+1 {
		t.Fatalf("table did not widen: %v <= %v", before.rect.W, e.bodyWidth())
	}
	res := e.resizers[0]
	bounds, _ := tt.Find("文档")
	sx, sy := bounds.X+res.x, bounds.Y+res.y+12
	tt.Move(sx, sy)
	tt.Frame()
	tt.Press(sx, sy)
	tt.Move(sx-400, sy)
	tt.Frame()
	tt.Release(sx-400, sy)
	tt.Frame()
	if len(e.Doc.Blocks[1].Widths) == 0 {
		t.Fatal("dragging a column boundary did not size the columns")
	}
	after := firstTable(t, e)
	if after.rect.W < before.rect.W-1 {
		t.Fatalf("resizing shrank the widened table back: %v -> %v", before.rect.W, after.rect.W)
	}
	floor := tableColumnMin(14)
	for i := 0; i+1 < len(after.cols); i++ {
		if w := after.cols[i+1] - after.cols[i]; w < floor-0.5 {
			t.Fatalf("column %d narrower than its text needs: %v < %v (%v)", i, w, floor, after.cols)
		}
	}
	for i := range e.rows {
		r := e.rows[i]
		if r.leaf.Owner != e.Doc.Blocks[1].ID || r.layout == nil {
			continue
		}
		if lw, _ := r.layout.Size(); r.x+lw > r.rect.X+r.rect.W+1 {
			t.Fatalf("cell %q spills past its column: text right %v > cell right %v", r.leaf.Block.Text(), r.x+lw, r.rect.X+r.rect.W)
		}
	}
}

func TestNarrowSizedColumnsKeepRoomForText(t *testing.T) {
	e, tt := newWideTester("| a | b | c |\n| --- | --- | --- |\n| 很长的一段文字 | 第二列 | 第三列 |", 900, Options{})
	e.Engine.SetWidths(e.Doc.Blocks[0].ID, []float32{2, 400, 400})
	tt.Frame()
	d := firstTable(t, e)
	floor := tableColumnMin(14)
	for i := 0; i+1 < len(d.cols); i++ {
		if w := d.cols[i+1] - d.cols[i]; w < floor-0.5 {
			t.Fatalf("column %d laid out at %v, under %v: %v", i, w, floor, d.cols)
		}
	}
	if d.rect.W > e.bodyWidth()+0.5 {
		t.Fatalf("flooring a narrow column grew the table past the column: %v > %v", d.rect.W, e.bodyWidth())
	}
}

func TestNoWideTablesKeepsTablesInTheTextColumn(t *testing.T) {
	e, _ := newWideTester(wideTableMD(), 1600, Options{NoWideTables: true})
	d := firstTable(t, e)
	if d.rect.X != e.padding || abs32(d.rect.W-e.bodyWidth()) > 0.5 {
		t.Fatalf("table left the text column with wide tables off: x=%v w=%v padding=%v body=%v", d.rect.X, d.rect.W, e.padding, e.bodyWidth())
	}
}

func TestFloorTableWidths(t *testing.T) {
	widths := []float32{10, 200, 100}
	if w := floorTableWidths(widths, 310, 52); w != 310 {
		t.Fatalf("total changed: %v", w)
	}
	if widths[0] != 52 || abs32(widths[0]+widths[1]+widths[2]-310) > 0.01 {
		t.Fatalf("floored widths %v", widths)
	}
	tight := []float32{10, 20}
	if w := floorTableWidths(tight, 30, 52); w != 104 || tight[0] != 52 || tight[1] != 52 {
		t.Fatalf("tight widths %v total %v", tight, w)
	}
}
