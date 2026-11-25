package editor

import (
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/egoist/mygo/ui"
)

func hoverTableWidth(t *testing.T, e *Editor, tt *ui.Tester) decoration {
	t.Helper()
	d := firstTable(t, e)
	bounds, _ := tt.Find("文档")
	tt.Move(bounds.X+d.rect.X+20, bounds.Y+d.rect.Y+20)
	tt.Frame()
	return d
}

func TestTableWidthRestoreButtonAndUndo(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	natural := firstTable(t, e).rect.W
	id := e.Doc.Blocks[1].ID
	manual := []float32{100, 100, 100, 100, 100, 100, 100, 120}
	e.SetWidths(id, manual)
	tt.Frame()
	d := hoverTableWidth(t, e, tt)
	if abs32(d.rect.W-820) > 1 {
		t.Fatalf("manual table width = %v, want 820", d.rect.W)
	}
	button, ok := tt.Find("恢复自适应宽度")
	if !ok {
		t.Fatal("manually sized table has no restore button")
	}
	// Moving from the table to its adjacent control must not hide it.
	tt.Move(button.X+12, button.Y+12)
	tt.Frame()
	if _, ok := tt.Find("恢复自适应宽度"); !ok {
		t.Fatal("restore button disappeared on approach")
	}
	tt.ClickAt(button.X+12, button.Y+12)
	tt.Frame()
	if len(e.blockByID(id).Widths) != 0 || abs32(firstTable(t, e).rect.W-natural) > 1 {
		t.Fatal("restore did not return to content-driven table width")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if !slices.Equal(e.blockByID(id).Widths, manual) {
		t.Fatal("undo did not restore the manual column widths")
	}
}

func TestTableOuterEdgesResizeAndSnap(t *testing.T) {
	for _, side := range []int{-1, 1} {
		name := "left"
		if side > 0 {
			name = "right"
		}
		t.Run(name, func(t *testing.T) {
			e, tt := newWideTester(wideTableMD(), 1600, Options{})
			d := hoverTableWidth(t, e, tt)
			natural := d.rect.W
			bounds, _ := tt.Find("文档")
			x := bounds.X + d.rect.X
			if side > 0 {
				x += d.rect.W
			}
			y := bounds.Y + d.rect.Y + 25
			tt.Move(x, y)
			tt.Frame()
			tt.Press(x, y)
			tt.Move(x-float32(side)*100, y)
			tt.Frame()
			preview := firstTable(t, e)
			if abs32(preview.rect.W-(natural-200)) > 1 {
				t.Fatalf("outer edge did not preview the new width: %v -> %v", natural, preview.rect.W)
			}
			if len(e.Doc.Blocks[1].Widths) != 0 {
				t.Fatal("drag preview wrote persistent widths before release")
			}
			tt.Release(x-float32(side)*100, y)
			tt.Frame()
			after := firstTable(t, e)
			if abs32(after.rect.W-(natural-200)) > 1 || len(e.Doc.Blocks[1].Widths) != 8 {
				t.Fatal("outer edge did not commit the preview width")
			}
			for i := 0; i+1 < len(d.cols); i++ {
				beforeShare := (d.cols[i+1] - d.cols[i]) / natural
				afterShare := (after.cols[i+1] - after.cols[i]) / after.rect.W
				if abs32(beforeShare-afterShare) > 0.001 {
					t.Fatalf("column %d changed proportion while resizing the table", i)
				}
			}
			tt.Key(ui.Cmd, ui.KeyZ)
			tt.Frame()
			if len(e.Doc.Blocks[1].Widths) != 0 || abs32(firstTable(t, e).rect.W-natural) > 1 {
				t.Fatal("one undo did not undo the entire drag")
			}
			tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
			tt.Frame()
			d = hoverTableWidth(t, e, tt)
			x = bounds.X + d.rect.X
			if side > 0 {
				x += d.rect.W
			}
			tt.Move(x, y)
			tt.Frame()
			tt.Press(x, y)
			tt.Move(x+float32(side)*97, y)
			tt.Release(x+float32(side)*97, y)
			tt.Frame()
			if len(e.Doc.Blocks[1].Widths) != 0 || abs32(firstTable(t, e).rect.W-natural) > 1 {
				t.Fatal("dragging near the adaptive width did not restore automatic sizing")
			}
			tt.SetSize(1200, 900)
			tt.Advance(windowResizeQuiet + time.Millisecond)
			if got := firstTable(t, e).rect.W; abs32(got-1040) > 1 {
				t.Fatalf("restored table did not follow the narrower page: %v", got)
			}
		})
	}
}

func TestTableWidthDragCancelAndKeyboard(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	d := hoverTableWidth(t, e, tt)
	label := "调整表格右边线"
	h, ok := tt.Find(label)
	if !ok {
		t.Fatal("table has no accessible outer-edge handle")
	}
	x, y := h.X+h.W/2, h.Y+25
	tt.Press(x, y)
	tt.Move(x-100, y)
	tt.Key(0, ui.KeyEscape)
	tt.Release(x-100, y)
	tt.Frame()
	if len(e.Doc.Blocks[1].Widths) != 0 || abs32(firstTable(t, e).rect.W-d.rect.W) > 1 {
		t.Fatal("Escape committed or retained the cancelled resize")
	}
	hoverTableWidth(t, e, tt)
	tt.ClickAt(x, y)
	tt.Frame()
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if abs32(firstTable(t, e).rect.W-(d.rect.W-20)) > 1 {
		t.Fatalf("keyboard could not reduce the table width: width=%v manual=%v drag=%+v", firstTable(t, e).rect.W, e.Doc.Blocks[1].Widths, e.tableWidthDrag)
	}
	// Keyboard focus must keep the controls alive when the pointer leaves.
	tt.Move(10, 10)
	tt.Frame()
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if abs32(firstTable(t, e).rect.W-(d.rect.W-40)) > 1 {
		t.Fatal("edge handle lost keyboard focus when the pointer left")
	}
	tt.Key(0, ui.KeyHome)
	tt.Frame()
	if len(e.Doc.Blocks[1].Widths) != 0 || abs32(firstTable(t, e).rect.W-d.rect.W) > 1 {
		t.Fatal("Home did not restore adaptive sizing")
	}
}

func TestTableWidthDragInterruptedByWindowResize(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	d := hoverTableWidth(t, e, tt)
	bounds, _ := tt.Find("文档")
	x, y := bounds.X+d.rect.X+d.rect.W, bounds.Y+d.rect.Y+25
	tt.Press(x, y)
	tt.Move(x-100, y)
	tt.Frame()
	tt.SetSize(900, 900)
	tt.Release(x-100, y)
	tt.SetSize(1600, 900)
	tt.Advance(windowResizeQuiet + time.Millisecond)
	if len(e.Doc.Blocks[1].Widths) != 0 || abs32(firstTable(t, e).rect.W-d.rect.W) > 1 {
		t.Fatal("window resize retained or committed an abandoned edge drag")
	}
}

func TestTableWidthDragCaptureCancellation(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	d := hoverTableWidth(t, e, tt)
	bounds, _ := tt.Find("文档")
	x, y := bounds.X+d.rect.X+d.rect.W, bounds.Y+d.rect.Y+25
	tt.Press(x, y)
	tt.Move(x-100, y)
	tt.CancelPress()
	tt.Frame()
	if len(e.Doc.Blocks[1].Widths) != 0 || abs32(firstTable(t, e).rect.W-d.rect.W) > 1 {
		t.Fatal("losing pointer capture retained or committed the preview")
	}
}

func TestTableWidthDragToProseAndReopen(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	d := hoverTableWidth(t, e, tt)
	bounds, _ := tt.Find("文档")
	x, y := bounds.X+d.rect.X+d.rect.W, bounds.Y+d.rect.Y+25
	tt.Press(x, y)
	tt.Move(x-600, y)
	tt.Release(x-600, y)
	tt.Frame()
	if got := firstTable(t, e).rect.W; abs32(got-820) > 1 {
		t.Fatalf("inward drag did not stop at the prose column: %v", got)
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := document.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reopened.Blocks[1].Widths, e.Doc.Blocks[1].Widths) {
		t.Fatal("saving lost the manual widths")
	}
	hoverTableWidth(t, e, tt)
	button, _ := tt.Find("恢复自适应宽度")
	tt.ClickAt(button.X+12, button.Y+12)
	tt.Frame()
	encoded, err = e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err = document.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Blocks[1].Widths) != 0 {
		t.Fatal("saving restored the discarded manual widths")
	}
}

func TestTableWidthStationaryClickKeepsManualWidths(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	manual := []float32{150, 200, 150, 180, 180, 190, 200, 190}
	e.SetWidths(e.Doc.Blocks[1].ID, manual)
	tt.Frame()
	d := hoverTableWidth(t, e, tt)
	bounds, _ := tt.Find("文档")
	x, y := bounds.X+d.rect.X+d.rect.W, bounds.Y+d.rect.Y+25
	tt.Press(x, y)
	tt.Frame()
	if !slices.Equal(firstTable(t, e).cols, d.cols) {
		t.Fatal("pressing the edge without dragging reset the column proportions")
	}
	tt.Release(x, y)
	tt.Frame()
	if !slices.Equal(e.Doc.Blocks[1].Widths, manual) {
		t.Fatal("clicking the edge changed manual widths")
	}
}

func TestTableWidthDragTracksPointerAfterClamping(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{})
	d := hoverTableWidth(t, e, tt)
	bounds, _ := tt.Find("文档")
	x, y := bounds.X+d.rect.X+d.rect.W, bounds.Y+d.rect.Y+25
	tt.Press(x, y)
	tt.Move(x-600, y)
	tt.Frame()
	tt.Move(x-100, y)
	tt.Frame()
	if got := firstTable(t, e).rect.W; abs32(got-(d.rect.W-200)) > 1 {
		t.Fatalf("edge no longer follows the pointer after clamping: %v", got)
	}
	tt.Move(x-3, y)
	tt.Release(x-3, y)
	tt.Frame()
	if len(e.Doc.Blocks[1].Widths) != 0 {
		t.Fatal("returning to the adaptive edge after crossing the minimum did not snap")
	}
}

func TestTableWidthControlsRespectReadingAndPageSettings(t *testing.T) {
	e, tt := newWideTester(wideTableMD(), 1600, Options{NoWideTables: true})
	e.SetWidths(e.Doc.Blocks[1].ID, []float32{200, 200, 200, 200, 200, 200, 200, 200})
	tt.Frame()
	hoverTableWidth(t, e, tt)
	if _, ok := tt.Find("调整表格右边线"); ok {
		t.Fatal("outer edge offered widening when wide tables are disabled")
	}
	b, ok := tt.Find("恢复自适应宽度")
	if !ok {
		t.Fatal("manual column widths cannot be reset with wide tables disabled")
	}
	tt.ClickAt(b.X+12, b.Y+12)
	tt.Frame()
	if len(e.Doc.Blocks[1].Widths) != 0 || abs32(firstTable(t, e).rect.W-e.bodyWidth()) > 1 {
		t.Fatal("reset ignored the wide-table preference")
	}
	e.SetWidths(e.Doc.Blocks[1].ID, []float32{100, 100, 100, 100, 100, 100, 100, 120})
	e.SetReadOnly(true)
	tt.Frame()
	hoverTableWidth(t, e, tt)
	if _, ok := tt.Find("恢复自适应宽度"); ok {
		t.Fatal("read-only table offers a width mutation")
	}
}
