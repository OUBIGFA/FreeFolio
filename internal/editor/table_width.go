package editor

import (
	"fmt"

	"github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// A drag previews layout without writing the document or adding undo entries.
// Keep the unsnapped width so small pointer moves can leave the snap zone.
type tableWidthDrag struct {
	id                 string
	width, autoWidth   float32
	minWidth, maxWidth float32
	widths             []float32
	moved, cancelled   bool
}

func (r tableWidthDrag) preview() (float32, bool) {
	w := max(r.minWidth, min(r.maxWidth, r.width))
	if abs32(w-r.autoWidth) <= 12 {
		return r.autoWidth, true
	}
	return w, false
}

func (e *Editor) tableColumnWidths(b *document.Block) []float32 {
	if r := e.tableWidthDrag; r.id == b.ID && r.moved && !r.cancelled {
		w, auto := r.preview()
		if auto {
			return nil
		}
		return tableWidthsAt(r.widths, w)
	}
	return b.Widths
}

// Persist absolute widths, as column resizing already does. The layout keeps
// their proportions and enforces its existing minimum cell widths.
func (e *Editor) setTableWidth(id string, widths []float32, width float32, auto bool) {
	var next []float32
	if !auto {
		next = tableWidthsAt(widths, width)
	}
	e.SetWidths(id, next)
	e.sync()
	e.ready = false
}

func tableWidthsAt(widths []float32, width float32) []float32 {
	total := float32(0)
	for _, w := range widths {
		total += w
	}
	next := make([]float32, len(widths))
	for i, w := range widths {
		next[i] = w * width / total
	}
	return next
}

func (e *Editor) tableWidthControls(c *ui.Context) {
	focused := e.tableWidthFocus
	e.tableWidthFocus = ""
	if e.ReadOnly || e.printing {
		return
	}
	for i := range e.visibleDecorations(40) {
		d := *e.indexedDecoration(i)
		if d.kind != "table" || len(d.cols) < 2 {
			continue
		}
		active := e.tableWidthDrag.id == d.id
		if !active && focused != d.id && !e.barsShown(d) && !e.hovered(d.rect, 40) {
			continue
		}
		b := e.blockByID(d.id)
		if b == nil {
			continue
		}
		dy := e.ownerVisualDY(d.id)
		if len(b.Widths) > 0 && !active && e.resizing == nil {
			// Keep the button above the right corner, clear of the column
			// insertion strip, bookmark and reserved scrollbar margin.
			button := markButton(c, e.page, "table:auto-width:"+d.id, iconFit, i18n.T("恢复自适应宽度"), false).
				Absolute().Left(min(e.width-40, d.rect.X+d.rect.W+8)).Top(d.rect.Y + dy - 36).
				Label(i18n.T("恢复自适应宽度"))
			if button.Focused() {
				e.tableWidthFocus = d.id
			}
			if button.Clicked() {
				e.setTableWidth(d.id, nil, 0, true)
				e.RequestFocus = true
			}
		}
		// Only top-level tables have room beyond the prose column. Nested,
		// indented and preference-constrained tables still offer the reset.
		parent, _ := findParentBlocks(&e.Doc.Blocks, d.id)
		if parent != &e.Doc.Blocks || b.Indent > 0 || e.noWideTables || e.width-160 <= e.bodyWidth() {
			if active {
				e.tableWidthDrag = tableWidthDrag{}
			}
			continue
		}
		_, autoWidth := e.tableSpanWidths(b, e.width, e.bodyWidth(), nil)
		widths := make([]float32, len(d.cols)-1)
		for col := range widths {
			widths[col] = d.cols[col+1] - d.cols[col]
		}
		for _, side := range []int{-1, 1} {
			x, label := d.rect.X, i18n.T("调整表格左边线")
			if side > 0 {
				x, label = d.rect.X+d.rect.W, i18n.T("调整表格右边线")
			}
			h := ui.Box(c.Key(fmt.Sprintf("table:edge:%s:%d", d.id, side))).Absolute().Left(x-4).Top(d.rect.Y+dy).Size(8, d.rect.H).
				Focusable().Role(ui.RoleSlider).Label(label).Value(fmt.Sprintf("%.0f", d.rect.W)).Cursor(ui.CursorResizeEW).
				Tooltip(i18n.T("拖动调整表格宽度，接近自适应宽度时吸附；Home 恢复自适应"))
			if h.Focused() {
				e.tableWidthFocus = d.id
			}
			if h.Hovered() || h.Focused() || active {
				color := e.page.Marker
				if active {
					if _, auto := e.tableWidthDrag.preview(); auto {
						color = c.Theme().Accent
					}
				}
				h.Draw(func(p *ui.Painter, rect ui.Rect) {
					p.Fill(ui.Rect{X: rect.X + 3, Y: rect.Y + 4, W: 2, H: max(0, rect.H-8)}, color, 1)
				})
			}
			if dx, _, held := h.Dragged(); held {
				if !active {
					e.tableWidthDrag = tableWidthDrag{id: d.id, width: d.rect.W, autoWidth: autoWidth, widths: widths,
						minWidth: e.bodyWidth(), maxWidth: e.width - 160}
					active = true
				}
				r := &e.tableWidthDrag
				if !r.cancelled && dx != 0 {
					r.width += float32(side) * 2 * dx
					r.moved = true
					e.ready = false
				}
			}
			if _, _, ended := h.DragEnded(); ended && e.tableWidthDrag.id == d.id {
				r := e.tableWidthDrag
				e.tableWidthDrag = tableWidthDrag{}
				if r.moved && !r.cancelled {
					w, auto := r.preview()
					e.setTableWidth(d.id, r.widths, w, auto)
					e.RequestFocus = true
				}
				e.ready = false
			}
			if h.PressCanceled() && e.tableWidthDrag.id == d.id {
				e.tableWidthDrag = tableWidthDrag{}
				e.ready = false
			}
			// DragEnded excludes a stationary click; clear that capture too.
			if h.Clicked() && e.tableWidthDrag.id == d.id {
				e.tableWidthDrag = tableWidthDrag{}
				e.ready = false
			}
			h.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputKeyDown {
					return false
				}
				if ev.Mods&ui.Cmd != 0 && ev.Key == ui.KeyZ {
					if ev.Mods&ui.Shift != 0 {
						e.Redo()
					} else {
						e.Undo()
					}
					e.tableWidthDrag.cancelled = true
					e.ready, e.RequestFocus = false, true
					return true
				}
				if ev.Mods != 0 {
					return false
				}
				switch ev.Key {
				case ui.KeyEscape:
					e.tableWidthDrag.cancelled = true
					e.ready = false
				case ui.KeyHome:
					e.tableWidthDrag.cancelled = true
					e.setTableWidth(d.id, nil, 0, true)
				case ui.KeyLeft, ui.KeyRight:
					e.tableWidthDrag.cancelled = true
					delta := float32(side) * 20
					if ev.Key == ui.KeyLeft {
						delta = -delta
					}
					w := max(e.bodyWidth(), min(e.width-160, d.rect.W+delta))
					e.setTableWidth(d.id, widths, w, false)
				default:
					return false
				}
				return true
			})
		}
		if e.tableWidthDrag.id == d.id && e.tableWidthDrag.moved && !e.tableWidthDrag.cancelled {
			if _, auto := e.tableWidthDrag.preview(); auto {
				ui.Text(c.Key("table:auto-width-hint:"+d.id), i18n.T("自适应宽度")).Absolute().
					Left(d.rect.X + d.rect.W - 112).Top(d.rect.Y + dy - 42).Height(24).PaddingX(8).
					FontSize(13).TextColor(e.page.OnScrim).Background(e.page.Scrim).Radius(4).PassThrough()
			}
		}
	}
}
