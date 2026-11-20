package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestOneBotResizeDragPersistsAndKeepsDraft(t *testing.T) {
	for _, left := range []bool{false, true} {
		t.Run(map[bool]string{false: "sidebar-right", true: "sidebar-left"}[left], func(t *testing.T) {
			a, tt := newTestApp(t)
			a.prefs.SidebarLeft = left
			a.qa.open = true
			a.qa.input = "keep my draft"
			tt.SetSize(1400, 850)
			settle(tt)
			edge, ok := tt.Find("调整 OneBot 宽度")
			if !ok {
				t.Fatal("missing divider")
			}
			x, y := edge.X+edge.W/2, edge.Y+edge.H/2
			tt.Press(x, y)
			tt.Move(x-50, y)
			tt.Frame()
			tt.Move(x-100, y)
			tt.Frame()
			tt.Release(x-100, y)
			settle(tt)
			if a.prefs.OneBotWidth != qaWidth+100 || a.qa.shown != qaWidth+100 {
				t.Fatalf("width %v shown %v", a.prefs.OneBotWidth, a.qa.shown)
			}
			p, err := a.prefStore.Load()
			if err != nil || p.OneBotWidth != qaWidth+100 {
				t.Fatal("width was not saved", p, err)
			}
			if a.qa.input != "keep my draft" {
				t.Fatal("resizing lost the draft")
			}
			a.qa.open = false
			settle(tt)
			a.qa.open = true
			settle(tt)
			if a.qa.shown != qaWidth+100 {
				t.Fatal("reopening lost width")
			}
		})
	}
}

func TestOneBotResizeKeyboardResetAndWindowBounds(t *testing.T) {
	a, tt := newTestApp(t)
	a.qa.open = true
	tt.SetSize(1400, 850)
	settle(tt)
	if err := tt.Click("调整 OneBot 宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyLeft)
	settle(tt)
	if a.qa.shown != qaWidth+10 {
		t.Fatal("keyboard resize", a.qa.shown)
	}
	tt.Key(0, ui.KeyEnter)
	settle(tt)
	if a.prefs.OneBotWidth != 0 || a.qa.shown != qaWidth {
		t.Fatal("default width not restored")
	}
	a.prefs.OneBotWidth = 800
	tt.SetSize(760, 620)
	settle(tt)
	docked := float32(0)
	if !a.folded() {
		docked = a.sidebarW()
	}
	if a.qa.shown > a.winW-docked-200 {
		t.Fatal("document squeezed out", a.qa.shown, a.winW, a.sidebarW())
	}
	if a.prefs.OneBotWidth != 800 {
		t.Fatal("temporary window constraint overwrote preferred width")
	}
	tt.SetSize(1600, 900)
	settle(tt)
	if a.qa.shown != 800 {
		t.Fatal("wide window did not restore preferred width", a.qa.shown)
	}
}

func TestOneBotDividerDoubleClickRestoresDefault(t *testing.T) {
	a, tt := newTestApp(t)
	a.qa.open = true
	a.prefs.OneBotWidth = 520
	tt.SetSize(1400, 850)
	settle(tt)
	edge, ok := tt.Find("调整 OneBot 宽度")
	if !ok {
		t.Fatal("missing divider")
	}
	x, y := edge.X+edge.W/2, edge.Y+edge.H/2
	for range 2 {
		tt.Press(x, y)
		tt.Frame()
		tt.Release(x, y)
		tt.Frame()
	}
	settle(tt)
	if a.prefs.OneBotWidth != 0 || a.qa.shown != qaWidth {
		t.Fatal("double-click did not restore default width")
	}
}
