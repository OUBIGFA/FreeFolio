package app

import (
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type quickNoteState struct {
	window          *docWindow
	opening         bool
	registered, err string
}

func (a *App) quickShortcut() string {
	if s := strings.TrimSpace(a.prefs.QuickNoteShortcut); s != "" {
		return s
	}
	return "CmdOrCtrl+Shift+Space"
}

func (a *App) configureQuickShortcut() {
	if a.win == nil {
		return
	}
	if a.quick.registered != "" {
		mygo.GlobalShortcut.Unregister(a.quick.registered)
		a.quick.registered = ""
	}
	a.quick.err = ""
	if !a.prefs.QuickNoteEnabled {
		return
	}
	key := a.quickShortcut()
	if err := mygo.GlobalShortcut.RegisterWith(key, mygo.GlobalShortcutOptions{Description: i18n.T("快速笔记")}, func() { a.win.Update(a.openQuickNote) }); err != nil {
		a.quick.err = err.Error()
		a.notify("无法注册快速笔记快捷键：%v", err)
		return
	}
	a.quick.registered = key
}

// Quick capture owns a normal resident document, so saving, attachments, IME,
// conflicts and quit handling all use the existing document lifecycle.
func (a *App) openQuickNote() {
	if a.quick.window != nil {
		a.quick.window.win.Focus()
		return
	}
	if a.quick.opening {
		return
	}
	sp := a.state.Get(a.prefs.QuickNoteSpace)
	if sp == nil {
		sp = a.state.ActiveSpace()
	}
	if sp == nil || sp.IsExternal() {
		a.notify("请先选择快速笔记的保存空间")
		return
	}
	folder := strings.TrimSpace(a.prefs.QuickNoteFolder)
	if folder == "" {
		folder = "Quick Notes"
	}
	if path.IsAbs(folder) || path.Clean(folder) != folder || strings.Contains(folder, "\\") || folder == ".." || strings.HasPrefix(folder, "../") {
		a.quick.err = i18n.T("快速笔记文件夹必须位于所选空间内")
		return
	}
	copySpace := *sp
	a.quick.opening = true
	a.backgroundResult(func() func() {
		rel, err := vault.CreateFile(copySpace.Root, folder, "Quick Note "+time.Now().Format("2006-01-02 15-04-05"), ".md", "")
		return func() {
			a.quick.opening = false
			if err != nil {
				a.quick.err = err.Error()
				a.notify("无法创建快速笔记：%v", err)
				return
			}
			d := openDocument(sp, rel, "", false)
			d.ed.RequestFocus = true
			w := &docWindow{doc: d}
			a.quick.window = w
			a.windows = append(a.windows, w)
			view := func(c *ui.Context) {
				c.Root().Font(a.interfaceFont())
				if a.autosaveEnabled() {
					d.autosaveWith(c, a.asyncPost())
				}
				a.withDocument(d, func() { a.cloudSaved(d) })
				a.windowView(c, w)
			}
			close := func() bool { return a.closeWindow(w) }
			if a.newWindow != nil {
				w.win = a.newWindow(i18n.T("快速笔记"), view, close)
			} else {
				win := newAppWindow(mygo.WindowOptions{Title: i18n.T("快速笔记"), Width: 560, Height: 440, MinWidth: 400, MinHeight: 300, StateKey: "quick-note", TitleBarStyle: mygo.TitleBarHidden}, view)
				win.OnClose(func(e *mygo.CloseEvent) {
					if !close() {
						e.PreventDefault()
					}
				})
				w.win = win
			}
			a.refreshTree()
		}
	})
}

func (a *App) settingsQuickNote(c *ui.Context) {
	settingsSection(c, i18n.T("快速笔记"))
	settingsGroup(c, "quick-note", func() {
		toggleRow(c, "quick-enabled", i18n.T("启用全局快速笔记"), i18n.T("FreeFolio 运行时，可从其他应用呼出独立笔记窗口"), &a.prefs.QuickNoteEnabled, func() { a.savePrefs(); a.configureQuickShortcut() })
		settingRow(c, "quick-key", i18n.T("全局快捷键"), "", func() {
			placeholder := "Ctrl+Shift+Space"
			if runtime.GOOS == "darwin" {
				placeholder = "Cmd+Shift+Space"
			}
			ui.TextInput(c.Key("key"), &a.prefs.QuickNoteShortcut).Placeholder(placeholder).Label(i18n.T("快速笔记快捷键")).Width(240)
			if ui.Button(c.Key("apply"), i18n.T("应用快捷键")).Clicked() {
				a.savePrefs()
				a.configureQuickShortcut()
			}
		})
		ids := []string{""}
		labels := []string{i18n.T("当前空间")}
		at := 0
		for _, sp := range a.state.Spaces {
			if !sp.IsExternal() {
				ids = append(ids, sp.ID)
				labels = append(labels, sp.Name+" · "+shortPath(sp.Root))
				if sp.ID == a.prefs.QuickNoteSpace {
					at = len(ids) - 1
				}
			}
		}
		settingRow(c, "quick-space", i18n.T("保存空间"), "", func() {
			choice := labels[at]
			before := choice
			ui.Select(c.Key("space"), &choice, labels).Label(i18n.T("快速笔记保存空间")).Width(240)
			if choice != before {
				for i, label := range labels {
					if choice == label {
						a.prefs.QuickNoteSpace = ids[i]
						break
					}
				}
				a.savePrefs()
			}
		})
		settingRow(c, "quick-folder", i18n.T("保存文件夹"), "", func() {
			before := a.prefs.QuickNoteFolder
			field := ui.TextInput(c.Key("folder"), &a.prefs.QuickNoteFolder).Placeholder("Quick Notes").Label(i18n.T("快速笔记文件夹")).Width(240)
			if field.Submitted() || before != a.prefs.QuickNoteFolder {
				a.savePrefs()
			}
		})
		if a.quick.err != "" {
			ui.Text(c, a.quick.err).TextColor(c.Theme().Danger).Padding(8)
		}
		if ui.Button(c.Key("open-quick-note"), i18n.T("打开快速笔记")).Clicked() {
			a.savePrefs()
			a.openQuickNote()
		}
	})
}
