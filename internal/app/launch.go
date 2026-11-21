package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// launchSettle is how long after the window's first frame its icon and the
// notification area's are set.
const launchSettle = 150 * time.Millisecond

// launch is what a launch leaves until the window shows. Setting the
// window's icon and making the tray icon take the system about a tenth of
// a second, which the first frame used to wait for.
type launch struct {
	pending bool
	// shown is when the first frame was built, and asked whether the
	// window's thread was asked to finish.
	shown time.Time
	asked bool
	// finish, when set, finishes the launch in place of the window's own
	// thread: tests answer it.
	finish func()
}

// warmIcons renders the logo at the sizes the window and the tray take, on
// a goroutine of its own while the window is made: iconPNG keeps them, so
// finishLaunch finds both ready.
func warmIcons() {
	windowIcon()
	trayIcon()
}

// launchTick finishes the launch once the first frame has had launchSettle
// to show.
func (a *App) launchTick(c *ui.Context) {
	l := &a.launch
	if !l.pending || l.asked {
		return
	}
	now := c.Now()
	if l.shown.IsZero() {
		l.shown = now
	}
	if wait := l.shown.Add(launchSettle).Sub(now); wait > 0 {
		c.After(wait)
		return
	}
	if l.finish != nil {
		l.pending = false
		l.finish()
		return
	}
	// Posted, so that this frame finishes before the system is asked.
	l.asked = true
	go a.win.Update(a.finishLaunch)
}

// finishLaunch sets the window's icon and shows the tray icon, once. Closing
// the window asks for it too: whether the window hides or the app quits
// depends on the tray icon being there.
func (a *App) finishLaunch() {
	if !a.launch.pending {
		return
	}
	a.launch.pending = false
	if err := a.win.SetIcon(windowIcon()); err != nil {
		a.notify("无法设置窗口图标。%v", err)
	}
	a.syncTray()
}
