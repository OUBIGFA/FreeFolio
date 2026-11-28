package app

import (
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/solar"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Client window controls, ported from ArcMD a51bce7: on KDE Wayland the
// windows ask for client decorations, and FreeFolio draws minimize, maximize
// and close in its own top bar instead of a separate KWin title row.

const (
	windowButtonsWidth = 3*30 + 2*8
	// captionBand is the height of the top band the buttons sit in.
	captionBand = 46
)

var (
	iconWindowMaximize = solar.Must("stop")
	iconWindowRestore  = solar.Must("copy")
)

type captionWindow interface {
	IsFullScreen() bool
	IsMaximized() bool
	Minimize()
	ToggleMaximize()
	Close()
}

var _ captionWindow = (*mygo.Window)(nil)

// captionWindow is the main window when FreeFolio draws its controls.
func (a *App) captionWindow() captionWindow {
	if a.testCaptionWindow != nil {
		return a.testCaptionWindow
	}
	if a.win != nil && hasAppWindowControls() {
		return a.win
	}
	return nil
}

// captionWidth is the room the client window controls take at the right of
// the top band, 0 when the system draws them or the window is full screen.
func (a *App) captionWidth() float32 {
	if win := a.captionWindow(); win != nil && !win.IsFullScreen() {
		return windowButtonsWidth + 8
	}
	return 0
}

// titleBar is the room the window controls take, the system's and FreeFolio's
// own alike, so every top bar keeps clear of them the same way.
func (a *App) titleBar(c *ui.Context) ui.TitleBar {
	bar := c.TitleBar()
	if w := a.captionWidth(); w > 0 {
		bar.Right += w
		bar.Height = max(bar.Height, captionBand)
		bar.Bottom = max(bar.Bottom, bar.Height)
	}
	return bar
}

// mainWindowButtons pins the controls to the window's top-right corner, over
// whatever page shows, so they stay reachable while panels animate.
func (a *App) mainWindowButtons(c *ui.Context) {
	if a.captionWidth() == 0 {
		return
	}
	h := float32(mainBarHeight)
	if a.focus || a.files != nil || a.split != nil || a.settingsOpen {
		h = captionBand
	}
	win := a.captionWindow()
	ui.Overlay(c.Key("caption-overlay"), func() {
		ui.Row(c.Key("caption-host")).Absolute().Right(8).Top((h - 30) / 2).Children(func() {
			windowButtons(c, win)
		})
	})
}

// windowButtons adds client-owned controls to an existing toolbar, rather
// than creating a title row above the application.
func windowButtons(c *ui.Context, win captionWindow) {
	if win == nil || win.IsFullScreen() {
		return
	}
	ui.Row(c.Key("window-controls")).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
		caption := func(key string, icon *ui.SVG, label string, action func()) {
			label = i18n.T(label)
			if iconButton(c, key, icon, label, label, false).Shrink(0).Clicked() {
				action()
			}
		}
		caption("minimize", iconMinus, "最小化窗口", win.Minimize)
		if win.IsMaximized() {
			caption("maximize", iconWindowRestore, "还原窗口", win.ToggleMaximize)
		} else {
			caption("maximize", iconWindowMaximize, "最大化窗口", win.ToggleMaximize)
		}
		caption("close", iconX, "关闭窗口", win.Close)
	})
}
