package app

import (
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

// Repeatedly squeezing a media-heavy canvas forces expensive GPU presentation
// during every sidebar animation frame. Keep its viewport and switch once.
func (ob *qaState) oneBotMotion() time.Duration {
	if ob.dragging || ob.resizing {
		return 0
	}
	if ob.host.boardOnScreen() {
		return 0
	}
	return slide
}

func (ob *qaState) oneBotWidth(docked float32) float32 {
	w := ob.host.preferences().OneBotWidth
	if w <= 0 {
		w = qaWidth
	}
	available := max(0, ob.host.windowWidth()-docked)
	if available == 0 {
		return w
	}
	minimum := min(float32(280), available/2)
	maximum := max(minimum, available-min(float32(240), available/2))
	return max(minimum, min(maximum, w))
}

// oneBotResizerTop is where the divider starts: under the header in the
// spaces, the panel's full height under the file bar.
func (ob *qaState) oneBotResizerTop(c *ui.Context) float32 {
	if ob.host.fileLocations() != nil {
		return 0
	}
	return ob.oneBotHeadHeight(c)
}

// The divider uses the existing native splitter interaction. Handle input is
// resolved before both panels are built, so dragging has no animated lag.
func (ob *qaState) oneBotResizer(c *ui.Context, docked float32) {
	if !ob.open || ob.shown < 1 || ob.qaActiveSpace() == nil {
		ob.dragging = false
		return
	}
	hint := i18n.T("拖动调整宽度；双击恢复默认；方向键微调")
	h := ui.Box(c.Key("onebot-resizer")).Absolute().Top(ob.oneBotResizerTop(c)).Bottom(8).Right(ob.shown).Width(8).
		Cursor(ui.CursorResizeEW).Focusable().Role(ui.RoleSplitter).Label(i18n.T("调整 OneBot 宽度")).Description(hint)
	set := func(w float32) {
		ob.host.preferences().OneBotWidth = w
		ob.host.preferences().OneBotWidth = ob.oneBotWidth(docked)
		if ob.host.preferences().OneBotWidth == qaWidth {
			ob.host.preferences().OneBotWidth = 0
		}
		ob.resizing = true
		c.After(0)
	}
	dx, _, held := h.Dragged()
	if held {
		if !ob.dragging {
			ob.dragging = true
			ob.dragWidth = ob.oneBotWidth(docked)
		}
		ob.dragWidth -= dx
		set(ob.dragWidth)
	} else if ob.dragging {
		ob.dragging = false
		ob.host.savePrefs()
	}
	switch {
	case h.DoubleClicked() || h.Shortcut(0, ui.KeyEnter):
		set(qaWidth)
		ob.dragWidth = ob.oneBotWidth(docked)
		ob.host.savePrefs()
	case h.Shortcut(0, ui.KeyLeft):
		set(ob.oneBotWidth(docked) + 10)
		ob.host.savePrefs()
	case h.Shortcut(0, ui.KeyRight):
		set(ob.oneBotWidth(docked) - 10)
		ob.host.savePrefs()
	}
	h.Right(ob.oneBotWidth(docked))
	resizeGrip(c, h, false, held || h.Hovered() || h.FocusVisible(), false)
	ob.host.resizerTip(c, h, hint)
}
