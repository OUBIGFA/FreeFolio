package editor

// Tables carry no controls around their grid: rows and columns are
// inserted, deleted, moved and sorted from a cell's context menu, cells
// selected across show a toolbar to merge, split or fill them, and the
// borders between columns resize them.

import (
	"fmt"
	"math"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func findParentBlocks(list *[]*document.Block, id string) (*[]*document.Block, int) {
	for i, b := range *list {
		if b.ID == id {
			return list, i
		}
		if b.Kind == "columns" || b.Kind == "column" {
			if p, idx := findParentBlocks(&b.Children, id); p != nil {
				return p, idx
			}
		}
	}
	return nil, -1
}

func (e *Editor) tableDecoration(id string) (decoration, bool) {
	if di, ok := e.decorationIDs[id]; ok {
		d := *e.indexedDecoration(di)
		if d.kind == "table" && d.id == id {
			return d, true
		}
	}
	return decoration{}, false
}

func clampedResizeDelta(r *columnHandle, resizeX float32, movedFar bool) (delta float32, snapped bool) {
	if r == nil || r.col < 0 || r.col+1 >= len(r.widths) {
		return 0, false
	}
	raw := resizeX - r.x
	lo := 48 - r.widths[r.col]
	hi := r.widths[r.col+1] - 48
	equalDelta := (r.widths[r.col+1] - r.widths[r.col]) * 0.5
	if equalDelta >= lo && equalDelta <= hi && abs32(raw-equalDelta) <= 6 && (abs32(equalDelta) > 1 || movedFar) {
		return equalDelta, true
	}
	return max(lo, min(raw, hi)), false
}

// cellEnter handles Enter inside a table cell: moves down to the same column,
// appends a new row at the bottom, or exits out of a trailing empty row.
func (e *Editor) cellEnter() bool {
	if e.ReadOnly || e.Anchor != e.Caret {
		return false
	}
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" {
		return false
	}
	table, rowIdx, colIdx, ok := e.Doc.CellAt(leaf.Block.ID)
	if !ok || table == nil || len(table.Children) == 0 {
		return false
	}
	spanCols, spanRows := document.Span(leaf.Block)
	nextRow := rowIdx + spanRows

	focusCellAt := func(t *document.Block, r, c int) bool {
		if r < 0 || r >= len(t.Children) || len(t.Children[r].Children) == 0 {
			return false
		}
		c = max(0, min(c, len(t.Children[r].Children)-1))
		target := t.Children[r].Children[c]
		for ci := c; ci >= 0 && target.ColSpan < 0; ci-- {
			target = t.Children[r].Children[ci]
		}
		for ri := r; ri >= 0 && target.RowSpan < 0; ri-- {
			target = t.Children[ri].Children[min(c, len(t.Children[ri].Children)-1)]
		}
		if l, ok := e.Engine.LeafByID(target.ID); ok {
			e.SetSelection(l.End, l.End)
			e.reveal = true
			return true
		}
		return false
	}

	if nextRow < len(table.Children) {
		return focusCellAt(table, nextRow, colIdx)
	}

	// Double-Enter on a trailing blank data row at column 0 exits the table
	// cleanly into a paragraph below.
	if rowIdx >= 1 && colIdx == 0 {
		rowEmpty := true
		for _, cell := range table.Children[rowIdx].Children {
			if strings.TrimSpace(cell.Text()) != "" {
				rowEmpty = false
				break
			}
		}
		if rowEmpty {
			tableID := table.ID
			trimTrailing := len(table.Children) > 2
			e.Engine.Edit(func() {
				if trimTrailing {
					e.Engine.TableDelete(tableID, true, rowIdx)
				}
				if parent, idx := findParentBlocks(&e.Doc.Blocks, tableID); parent != nil {
					next := document.NewBlock("paragraph", "")
					*parent = append((*parent)[:idx+1], append([]*document.Block{next}, (*parent)[idx+1:]...)...)
					e.Engine.FocusBlock(next.ID)
				}
			})
			e.sync()
			e.reveal = true
			return true
		}
	}

	numCols := len(table.Children[rowIdx].Children)
	targetCol := colIdx
	if colIdx+spanCols >= numCols && numCols > 1 && strings.TrimSpace(table.Children[rowIdx].Children[0].Text()) != "" {
		targetCol = 0
	}
	tableID := table.ID
	e.Engine.ResizeTable(true, 1)
	e.sync()
	for _, b := range e.Doc.Blocks {
		if b.ID == tableID {
			return focusCellAt(b, len(b.Children)-1, targetCol)
		}
	}
	if updated, _, _, ok := e.Doc.CellAt(leaf.Block.ID); ok {
		return focusCellAt(updated, len(updated.Children)-1, targetCol)
	}
	return true
}

// paintTables draws, within each table in view, the cells a selection runs
// across and the live feedback of a column being resized.
func (e *Editor) paintTables(p *ui.Painter, bounds ui.Rect) {
	pg := e.page
	for i := range e.visibleDecorations(24) {
		d := *e.indexedDecoration(i)
		if d.kind != "table" || d.rect.Y+d.rect.H < e.Scroll.Y || d.rect.Y > e.Scroll.Y+e.viewport || len(d.cols) < 2 {
			continue
		}
		ownerDY := e.ownerVisualDY(d.id)
		offset := func(r ui.Rect) ui.Rect {
			r.X += bounds.X
			r.Y += bounds.Y + ownerDY
			return r
		}
		if id, r, ok := e.cellRange(); ok && id == d.id {
			cells := ui.Rect{X: d.cols[r.Col0], Y: d.rows[r.Row0], W: d.cols[r.Col1+1] - d.cols[r.Col0], H: d.rows[r.Row1+1] - d.rows[r.Row0]}
			p.Clip(offset(d.rect), 8, func() { p.Fill(offset(cells), pg.CellSelected, 0) })
		}
		if e.resizing != nil && e.resizing.id == d.id {
			e.paintColumnResizePreview(p, bounds, d, ownerDY)
		}
	}
}

func (e *Editor) paintColumnResizePreview(p *ui.Painter, bounds ui.Rect, d decoration, ownerDY float32) {
	r := e.resizing
	if r == nil || r.col < 0 || r.col+1 >= len(r.widths) {
		return
	}
	delta, snapped := clampedResizeDelta(r, e.resizeX, e.resizeMoved)
	lineX := bounds.X + r.x + delta
	topY := bounds.Y + d.rect.Y + ownerDY
	lineColor := e.page.Marker
	if snapped {
		lineColor = p.Theme().Accent
	}
	p.Fill(ui.Rect{X: lineX - 1, Y: topY, W: 2, H: d.rect.H}, lineColor, 1)

	w0 := r.widths[r.col] + delta
	w1 := r.widths[r.col+1] - delta
	total := max(1, r.width)
	pct0 := int(math.Round(float64(w0 * 100 / total)))
	pct1 := int(math.Round(float64(w1 * 100 / total)))
	label := fmt.Sprintf("%d%% · %d%%", pct0, pct1)
	span := ui.Span{Text: label, Size: 11.5, Weight: 600, Color: e.page.OnScrim, Features: "tnum"}
	tw, th := p.MeasureText(0, span)
	pw, ph := tw+14, float32(22)
	pill := ui.Rect{
		X: max(bounds.X+d.rect.X, min(lineX-pw/2, bounds.X+d.rect.X+d.rect.W-pw)),
		Y: topY - ph - 6,
		W: pw,
		H: ph,
	}
	p.Fill(pill, e.page.Scrim, 6)
	if snapped {
		p.Stroke(pill, lineColor.Alpha(0.85), 6, 1)
	}
	p.RichText(pill.X+7, pill.Y+(ph-th)/2, 0, span)
}

// cellRange is the rectangle of cells a text selection spans when it
// runs across two or more cells of one table, grown over merged cells.
func (e *Editor) cellRange() (string, document.CellRange, bool) {
	sel := e.Selection().Range
	if sel.Start == sel.End {
		return "", document.CellRange{}, false
	}
	a, z := e.Engine.LeafAt(sel.Start), e.Engine.LeafAt(sel.End)
	if a.Block == z.Block || a.Block.Kind != "cell" || z.Block.Kind != "cell" || a.Owner != z.Owner {
		return "", document.CellRange{}, false
	}
	table, r0, c0, ok0 := e.Doc.CellAt(a.Block.ID)
	_, r1, c1, ok1 := e.Doc.CellAt(z.Block.ID)
	if !ok0 || !ok1 {
		return "", document.CellRange{}, false
	}
	r := document.CellRange{Row0: min(r0, r1), Col0: min(c0, c1), Row1: max(r0, r1), Col1: max(c0, c1)}
	return table.ID, document.Expand(table, r), true
}

// caretCell is the merged cell holding the caret, if any.
func (e *Editor) caretCell() (*document.Block, bool) {
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" {
		return nil, false
	}
	cols, rows := document.Span(leaf.Block)
	return leaf.Block, cols > 1 || rows > 1
}

// cellFillButton opens the cell background panel.
func (e *Editor) cellFillButton(c *ui.Context) {
	b := ui.ButtonBase(c.Key("table:fill")).Size(24, 24).Radius(4).Center().Label(i18n.T("单元格背景颜色")).Tooltip(i18n.T("单元格背景颜色")).KeepFocus().Cursor(ui.CursorPointer)
	if b.Hovered() || e.cellFillOpen {
		b.Background(e.page.Hover)
	}
	b.Children(func() { ui.Icon(c, iconPaintBucket).FontSize(18).TextColor(e.page.CodeText) })
	if b.Clicked() {
		e.cellFillOpen, e.cellFillOpened = !e.cellFillOpen, true
	}
}

// cellFillPanel picks the background of the cells of r in table id.
func (e *Editor) cellFillPanel(c *ui.Context, x, y float32, id string, r document.CellRange, current string) {
	if !e.cellFillOpen {
		return
	}
	t := c.Theme()
	pg := e.page
	panel := ui.Column(c.Key("cell-fill-panel")).Absolute().Left(max(8, x)).Top(y).Width(230).Padding(4, 10, 12, 10)
	e.popover(c, panel).Children(func() {
		ui.Text(c, i18n.T("单元格背景颜色")).FontSize(12).TextColor(t.Text).Margin(7, 0, 4, 0)
		names := append([]string{""}, document.TextHighlights...)
		ui.Column(c).Gap(4).Children(func() {
			for row := 0; row < len(names); row += 8 {
				ui.Row(c.Key(row)).Justify(ui.SpaceBetween).Children(func() {
					for _, name := range names[row:min(row+8, len(names))] {
						label := i18n.T("无背景颜色")
						if name != "" {
							label = i18n.T("单元格背景：") + ColorName(name, true)
						}
						s := ui.ButtonBase(c.Key("cf:"+name)).Size(26, 26).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
						if current == name {
							s.Border(2, pg.CodeHover)
						} else if s.Hovered() {
							s.Background(pg.Hover)
						}
						s.Children(func() {
							box := ui.Box(c).Size(22, 22).Radius(2).Border(1, pg.Hover)
							if name == "" {
								box.Background(t.Background).Center().Children(func() {
									ui.Icon(c, iconSlash).FontSize(20).TextColor(pg.Placeholder)
								})
							} else {
								box.Background(pg.CellFills[name])
							}
						})
						if s.Clicked() {
							e.Engine.SetCellBackground(id, r, name)
							e.cellFillOpen = false
							e.RequestFocus = true
						}
					}
				})
			}
		})
	})
	if panel.PressedOutside() && !e.cellFillOpened {
		e.cellFillOpen = false
	}
	e.cellFillOpened = false
}

// tableToolbar floats over cells a selection runs across: merge, split
// and their background.
func (e *Editor) tableToolbar(c *ui.Context, surface ui.Element) {
	if id, r, ok := e.cellRange(); ok && e.focused && !e.dragging {
		e.cellToolbar(c, surface.Bounds(), id, r)
		return
	}
	e.cellFillOpen = false
}

// cellToolbar floats over cells selected across: merge, split and their
// background.
func (e *Editor) cellToolbar(c *ui.Context, bounds ui.Rect, id string, r document.CellRange) {
	d, _ := e.tableDecoration(id)
	if len(d.cols) <= r.Col1+1 || len(d.rows) <= r.Row1+1 {
		return
	}
	table, _, _, _ := e.Doc.CellAt(e.Engine.LeafAt(e.Selection().Range.Start).Block.ID)
	if table == nil {
		return
	}
	merged := false
	for ri := r.Row0; ri <= r.Row1; ri++ {
		for ci := r.Col0; ci <= r.Col1; ci++ {
			if cols, rows := document.Span(table.Children[ri].Children[ci]); cols > 1 || rows > 1 {
				merged = true
			}
		}
	}
	single := merged && func() bool {
		cols, rows := document.Span(table.Children[r.Row0].Children[r.Col0])
		return r.Row1-r.Row0+1 == rows && r.Col1-r.Col0+1 == cols
	}()
	x, y := d.cols[r.Col0], d.rows[r.Row0]-46
	current := table.Children[r.Row0].Children[r.Col0].Background
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("cell-toolbar")).Absolute().Left(bounds.X + x).Top(bounds.Y + y).Height(40).PaddingX(8).Gap(8).AlignItems(ui.Center)
		e.popover(c, bar).Children(func() {
			if !single && markButton(c, e.page, "cell:merge", iconMerge, i18n.T("合并单元格"), false).Clicked() {
				e.Engine.MergeCells(id, r)
				e.sync()
				e.RequestFocus = true
			}
			if merged && markButton(c, e.page, "cell:split", iconSplit, i18n.T("拆分单元格"), false).Clicked() {
				e.Engine.Edit(func() {
					for ri := r.Row0; ri <= r.Row1; ri++ {
						for ci := r.Col0; ci <= r.Col1; ci++ {
							if cell := table.Children[ri].Children[ci]; cell.ColSpan > 1 || cell.RowSpan > 1 {
								e.Engine.SplitCell(cell.ID)
							}
						}
					}
				})
				e.sync()
				e.RequestFocus = true
			}
			e.cellFillButton(c)
		})
		e.cellFillPanel(c, bounds.X+x, bounds.Y+y+44, id, r, current)
	})
}

// MergeSelectedCells merges the cells the selection runs across.
func (e *Editor) MergeSelectedCells() {
	if id, r, ok := e.cellRange(); ok {
		e.Engine.MergeCells(id, r)
		e.sync()
	}
}

// SplitCaretCell splits the merged cell holding the caret.
func (e *Editor) SplitCaretCell() {
	if cell, ok := e.caretCell(); ok {
		e.Engine.SplitCell(cell.ID)
		e.sync()
	}
}

// SortCaretColumn sorts the table holding the caret by the caret's column.
// It reports whether there was a table it could sort.
func (e *Editor) SortCaretColumn(descending bool) bool {
	cell, ok := e.caretCell()
	if !ok {
		return false
	}
	table, _, col, ok := e.Doc.CellAt(cell.ID)
	if !ok || !e.Doc.CanSortTable(table.ID) {
		return false
	}
	e.Engine.SortTable(table.ID, col, descending)
	e.sync()
	return true
}
