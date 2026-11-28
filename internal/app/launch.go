package app

import (
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// launchSettle is how long after the window's first frame its icon and the
// notification area's are set.
const launchSettle = 150 * time.Millisecond

// launch is what a launch leaves until the window shows. Setting the
// window's icon and making the tray icon take the system about a tenth of
// a second, which the first frame used to wait for.
type launch struct {
	pending       bool
	warmed        bool
	savePrefs     bool
	loadPlugins   bool
	migrateSpaces bool
	deferSearch   bool
	deferTree     bool
	boards        []*document // Parse restored boards after the window appears.
	// shown is when the first frame was built, and asked whether the
	// window's thread was asked to finish.
	shown time.Time
	asked bool
	// finish, when set, finishes the launch in place of the window's own
	// thread: tests answer it.
	finish func()
}

// warmIcons renders the logo at the sizes the window and the tray take, on
// a goroutine of its own after the first frame is scheduled: iconPNG keeps
// them, so finishLaunch finds both ready.
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
		if !l.warmed && a.win != nil && l.finish == nil {
			l.warmed = true
			go warmIcons()
		}
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

// finishLaunch sets the window's icon, shows the tray icon, and starts
// deferred background startup work once the first frame is on screen.
func (a *App) finishLaunch() {
	if a.launch.pending {
		a.launch.pending = false
		if a.win != nil {
			if err := a.win.SetIcon(windowIcon()); err != nil {
				a.notify("无法设置窗口图标；%v", err)
			}
			a.syncTray()
			a.configureQuickShortcut()
		}
	}
	a.runDeferredStartup()
}

func (a *App) runDeferredStartup() {
	if a.launch.deferTree {
		a.launch.deferTree = false
		a.syncTree()
	}
	boards := a.launch.boards
	a.launch.boards = nil
	for _, d := range boards {
		if !d.closed.Load() && a.isResidentDocument(d) {
			a.loadWhiteboardForView(d)
		}
	}
	if a.win != nil {
		a.gitSync.loadGitSync()
	}
	if a.imports.offer && !a.imports.discovering && a.win != nil {
		if sp := a.state.ActiveSpace(); sp != nil {
			a.imports.spaceID = sp.ID
		}
		a.discoverLibraries()
	}
	if a.prefs.BodyFont != "" || a.prefs.InterfaceFont != "" {
		a.fonts.loadFonts()
	}
	if a.launch.savePrefs {
		a.launch.savePrefs = false
		if !a.prefsBroken {
			a.savePrefs()
		}
	}
	if a.quitting {
		return
	}
	a.chatgpt.loadChatGPTSession(a.prefs.AI.Active, false)
	a.syncWatches()
	if a.launch.deferSearch {
		a.launch.deferSearch = false
		a.refreshSearch()
	}
	if a.launch.loadPlugins {
		a.launch.loadPlugins = false
		if a.win != nil {
			win := a.win
			plugins := a.plugins
			go func() {
				installed, broken, err := plugins.Scan()
				win.Update(func() {
					if err != nil {
						a.notify("读取插件文件夹失败；%v", err)
						return
					}
					a.installed, a.brokenPlugins = installed, broken
					a.syncFormatPlugins()
				})
			}()
		} else {
			a.reloadPlugins()
		}
	}
	if a.launch.migrateSpaces {
		a.launch.migrateSpaces = false
		type spaceTarget struct{ name, root string }
		var targets []spaceTarget
		for _, sp := range a.state.Spaces {
			if !sp.IsExternal() && sp.Root != "" {
				targets = append(targets, spaceTarget{sp.Name, sp.Root})
			}
		}
		if len(targets) == 0 {
			return
		}
		type migrateErr struct {
			name string
			err  error
		}
		run := func() []migrateErr {
			var errs []migrateErr
			for _, sp := range targets {
				if err := vault.MigrateSystemDir(sp.root); err != nil {
					errs = append(errs, migrateErr{sp.name, err})
				}
			}
			return errs
		}
		if a.win != nil {
			win := a.win
			go func() {
				errs := run()
				if len(errs) > 0 {
					win.Update(func() {
						for _, e := range errs {
							a.notify("迁移空间 %s 的系统文件夹失败；%v", e.name, e.err)
						}
					})
				}
			}()
		} else {
			for _, e := range run() {
				a.notify("迁移空间 %s 的系统文件夹失败；%v", e.name, e.err)
			}
		}
	}
}
