package app

import (
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func (a *App) oneBotWidth(docked float32) float32 {
	w := a.prefs.OneBotWidth
	if w <= 0 {
		w = qaWidth
	}
	available := max(0, a.winW-docked)
	if available == 0 {
		return w
	}
	minimum := min(float32(280), available/2)
	maximum := max(minimum, available-min(float32(240), available/2))
	return max(minimum, min(maximum, w))
}

// The divider uses the existing native splitter interaction. Handle input is
// resolved before both panels are built, so dragging has no animated lag.
func (a *App) oneBotResizer(c *ui.Context, docked float32) {
	if !a.qa.open || a.qa.shown < 1 || a.state.ActiveSpace() == nil {
		a.qa.dragging = false
		return
	}
	hint := i18n.T("拖动调整宽度；双击恢复默认；方向键微调")
	h := ui.Box(c.Key("onebot-resizer")).Absolute().Top(max(c.TitleBar().Height, 46)).Bottom(8).Right(a.qa.shown).Width(8).
		Cursor(ui.CursorResizeEW).Focusable().Role(ui.RoleSplitter).Label(i18n.T("调整 OneBot 宽度")).Description(hint)
	set := func(w float32) {
		a.prefs.OneBotWidth = w
		a.prefs.OneBotWidth = a.oneBotWidth(docked)
		if a.prefs.OneBotWidth == qaWidth {
			a.prefs.OneBotWidth = 0
		}
		a.qa.resizing = true
		c.After(0)
	}
	dx, _, held := h.Dragged()
	if held {
		if !a.qa.dragging {
			a.qa.dragging = true
			a.qa.dragWidth = a.oneBotWidth(docked)
		}
		a.qa.dragWidth -= dx
		set(a.qa.dragWidth)
	} else if a.qa.dragging {
		a.qa.dragging = false
		a.savePrefs()
	}
	switch {
	case h.DoubleClicked() || h.Shortcut(0, ui.KeyEnter):
		set(qaWidth)
		a.qa.dragWidth = a.oneBotWidth(docked)
		a.savePrefs()
	case h.Shortcut(0, ui.KeyLeft):
		set(a.oneBotWidth(docked) + 10)
		a.savePrefs()
	case h.Shortcut(0, ui.KeyRight):
		set(a.oneBotWidth(docked) - 10)
		a.savePrefs()
	}
	h.Right(a.oneBotWidth(docked))
	resizeGrip(c, h, false, held || h.Hovered() || h.FocusVisible(), false)
	a.resizerTip(c, h, hint)
}
