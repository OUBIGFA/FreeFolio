package app

import (
	"fmt"
	"math"
	"os"
	"slices"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo/ui"
)

// Settings tabs.
const (
	tabCloud      = "cloud-sync"
	tabGit        = "git-sync"
	tabGeneral    = "general"
	tabAppearance = "appearance"
	tabSpaces     = "spaces"
	tabShortcuts  = "shortcuts"
	tabAI         = "ai"
	tabAgents     = "local-agents"
	tabResources  = "resources"
	tabWriting    = "writing"
	tabFiles      = "files"
	tabUpdates    = "updates"
	tabAbout      = "about"
)

// openSettings shows the settings on a tab.
func (a *App) openSettings(tab string) {
	a.settingsOpen = true
	a.settingsTab = tab
	if tab == tabGit {
		a.gitSync.initGitSync()
		a.gitSync.loadGitSync()
	}
	if tab == tabAI {
		a.chatgpt.loadChatGPTSession(a.prefs.AI.Active, false)
	}
	a.refreshAgentSkillsCache()
}

// moreMenu keeps document actions at hand. Library and settings lead to
// app-wide tools without repeating their individual pages here.
func (a *App) moreMenu(c *ui.Context) {
	b := iconButton(c, "more", iconEllipsis, i18n.T("更多"), i18n.T("更多"), false).Role(ui.RoleMenuButton).Shrink(0)
	b.PopoverMenuButton(func(m *ui.Menu) {
		noDoc := a.doc == nil || a.doc.loadErr != nil
		noText := noDoc || a.doc.isPDF()
		isMDZ := a.doc != nil && a.doc.isMDZ()
		isDB := a.doc != nil && a.doc.isDatabase()
		if a.menuKey(m.Item(i18n.T("专注模式")), "focus").Checked(a.focus).Chosen() {
			a.toggleFocus()
		}
		if a.menuKey(m.Item(i18n.T("目录")), "outline").Checked(a.outlineOpen()).Disabled(noDoc || a.doc.source || isDB || (isMDZ && !a.doc.mdz.hasActivePage())).Chosen() {
			a.toggleOutline()
		}
		mode := i18n.T("Markdown 源码")
		if a.doc != nil && a.doc.source {
			mode = i18n.T("返回富文本编辑")
		}
		if a.menuKey(m.Item(mode), "source").Disabled(noText || isMDZ || isDB).Chosen() {
			a.toggleSource()
		}
		m.Submenu(i18n.T("页面宽度"), a.pageWidthMenu).Disabled(!canAdjustPageWidth(a.doc))
		if a.menuKey(m.Item(i18n.T("添加分屏")), "split-add").Disabled(a.doc == nil).Chosen() {
			a.addSplitPane()
		}
		m.Separator()
		if a.menuKey(m.Item(i18n.T("版本历史")), "history").Disabled(noText || isMDZ).Chosen() {
			a.versions.show()
		}
		m.Submenu(i18n.T("导出"), func(sub *ui.Menu) {
			if isDB {
				if sub.Item(i18n.T("导出 CSV 文件")).Chosen() {
					_, _ = a.databases.exportDatabaseCSV(a.doc)
				}
			}
			if sub.Item(i18n.T("导出 Markdown 文件")).Disabled(noText || isMDZ || isDB).Chosen() {
				a.exportMarkdown()
			}
			if a.menuKey(sub.Item(i18n.T("导出 PDF")), "export-pdf").Disabled(noText || isMDZ || isDB).Chosen() {
				a.pdfExports.exportPDF()
			}
			if sub.Item(i18n.T("导出 HTML 网页")).Disabled(noText || isMDZ || isDB).Chosen() {
				a.exportHTML()
			}
			if sub.Item(i18n.T("导出 MDZip (.mdz)")).Disabled(noText || isMDZ || isDB).Chosen() {
				a.mdzFiles.exportMDZ()
			}
		}).Disabled(noText || isMDZ)
		m.Submenu(i18n.T("页面缩放"), a.zoom.pageZoomMenu).Disabled(!a.zoom.canZoomPage(a.doc))
		if m.Item(i18n.T("打印")).Disabled(noText || isMDZ || isDB).Chosen() {
			a.pdfExports.printDoc()
		}
		if m.Item(i18n.T("在文件管理器中显示文档")).Disabled(noDoc).Chosen() {
			a.revealIn(a.doc.space, a.doc.rel)
		}
		m.Separator()
		if m.Item(i18n.T("关闭文档")).Shortcut(ui.Cmd, ui.KeyW).Disabled(a.doc == nil).Chosen() {
			a.closeShownDoc()
		}
		if m.Item(i18n.T("打开文件")).Chosen() {
			a.chooseExternalFiles()
		}
		if m.Item(i18n.T("另存为")).Disabled(noText).Chosen() {
			a.chooseSaveAs()
		}
		if a.menuKey(m.Item(i18n.T("资料库")), "library").Chosen() {
			a.library.toggleLibrary()
		}
		if a.menuKey(m.Item(i18n.T("设置")), "settings").Chosen() {
			a.openSettings(tabGeneral)
		}
		if m.Item(i18n.T("GitHub 同步")).Chosen() {
			a.openSettings(tabGit)
		}
	})
}

// settingsTabs are the settings' pages in the navigation's order.
func settingsTabs() []struct{ id, label, icon string } {
	return []struct{ id, label, icon string }{
		{tabGeneral, i18n.T("通用"), "settings"},
		{tabWriting, i18n.T("写作"), "pencil"},
		{tabAppearance, i18n.T("外观"), "palette"},
		{tabFiles, i18n.T("文件浏览"), "files"},
		{tabSpaces, i18n.T("空间"), "layers"},
		{tabAI, i18n.T("OneBot"), "ask"},
		{tabAgents, i18n.T("本地 Agent 接入"), "code"},
		{tabResources, i18n.T("可选资源"), "folder"},
		{tabCloud, i18n.T("云端同步"), "cloud"},
		{tabGit, i18n.T("GitHub 同步"), "git"},
		{tabShortcuts, i18n.T("快捷键"), "keyboard"},
		{tabUpdates, i18n.T("更新"), "refresh"},
		{tabAbout, i18n.T("关于"), "info"},
	}
}

// settingsView is a page of its own over the window: the navigation on
// the chrome, the chosen page on a white pane under a breadcrumb. The way
// back sits at the navigation's head, beside its title.
func (a *App) settingsView(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	defer a.fonts.fontPickerView(c)
	defer a.cleanupView(c)
	t := c.Theme()
	w, h := c.Size()
	bar := a.titleBar(c)
	rail := float32(212)
	if w < 800 {
		rail = 148
	}
	compact := w < 720
	if compact {
		rail = 0
	}
	tabs := settingsTabs()
	current := ""
	for _, tab := range tabs {
		if tab.id == a.settingsTab {
			current = tab.label
		}
	}
	back := func() {
		if iconButton(c, "settings-back", iconBack, i18n.T("返回"), i18n.T("返回")+" (Esc)", false).Clicked() {
			a.settingsOpen = false
		}
	}
	ui.Overlay(c.Key("settings"), func() {
		ui.Column(c.Key("settings-surface")).Fill().Modal().Role(ui.RoleDialog).Label(i18n.T("设置")).Disabled(a.updates.installing).Background(t.Background).Children(func() {
			ui.Box(c.Key("settings-title")).Height(max(12, bar.Height)).DragWindow()
			ui.Row(c.Key("settings-body")).Grow(1).MinHeight(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
				if !compact {
					ui.Column(c.Key("settings-nav")).Width(rail).Shrink(0).Padding(0, 16, 16, 16).Gap(12).Children(func() {
						// The way back heads the navigation, beside its title.
						ui.Row(c.Key("settings-back-row")).Gap(8).AlignItems(ui.Center).Children(func() {
							back()
							ui.Text(c, i18n.T("设置")).FontSize(15).FontWeight(600)
						})
						ui.Sidebar(c.Key("settings-tabs"), &a.settingsTab, func() {
							for _, tab := range tabs {
								ui.SidebarItem(c, tab.id, settingsIcons[tab.icon], tab.label).Height(grid4(min(40, h/14)))
							}
						}).Label(i18n.T("设置")).Grow(1).Shrink(1).MinHeight(0).Padding(0).Gap(4).Background(ui.Transparent)
					})
				}
				if !a.updates.installing && a.recording == "" && c.Shortcut(0, ui.KeyEscape) {
					a.settingsOpen = false
				}
				pane := ui.Column(c.Key("settings-pane")).Grow(1).MinWidth(0).Background(t.Surface).Radius(panelRadius).Margin(0, 12, 12, 0).Clip()
				if compact {
					pane.Margin(0).Radius(0)
				}
				pane.Children(func() {
					// The page's own title names it, so only the compact layout,
					// which has no navigation, keeps a header: back and a page menu.
					if compact {
						ui.Row(c.Key("settings-header")).Height(48).Shrink(0).PaddingX(16).AlignItems(ui.Center).Children(func() {
							back()
							ui.Text(c, i18n.T("设置")).FontSize(14).FontWeight(600).PaddingX(8)
							ui.Spacer(c)
							button := ui.Button(c.Key("settings-pages"), "").Role(ui.RoleMenuButton).Label(i18n.T("选择设置页面")).Gap(12).Children(func() {
								ui.Text(c, current).SingleLine()
								ui.Icon(c, iconChevronDown).FontSize(13)
							})
							button.PopoverMenuButton(func(menu *ui.Menu) {
								for _, tab := range tabs {
									if menu.Item(tab.label).Checked(tab.id == a.settingsTab).Chosen() {
										a.settingsTab = tab.id
									}
								}
							})
						})
					}
					container := ui.Scroll
					if a.settingsTab == tabSpaces || a.settingsTab == tabShortcuts {
						container = ui.Column
					}
					padY, padX := grid4(min(48, h/16)), grid4(min(48, w/24))
					container(c.Key("content")).Grow(1).MinHeight(0).Padding(padY, padX).AlignItems(ui.Center).Children(func() {
						ui.Column(c.Key("settings-page-" + a.settingsTab)).Width(max(0, min(680, w-rail-12-2*padX))).Grow(1).MinHeight(0).Gap(grid4(min(16, h/40))).Children(func() {
							switch a.settingsTab {
							case tabAppearance:
								a.settingsAppearance(c)
							case tabSpaces:
								a.settingsSpaces(c)
							case tabShortcuts:
								a.settingsShortcuts(c)
							case tabWriting:
								a.settingsWriting(c)
							case tabFiles:
								a.settingsFiles(c)
							case tabUpdates:
								a.settingsUpdates(c)
							case tabCloud:
								a.cloud.settingsCloudSync(c)
							case tabGit:
								a.gitSync.settingsGitSync(c)
							case tabAI:
								a.qa.settingsAI(c)
							case tabAgents:
								a.settingsLocalAgents(c, func() { settingsSeparator(c) }, func(key, label, note string, control func()) { settingRow(c, key, label, note, control) })
							case tabResources:
								a.resources.settingsResources(c)
							case tabAbout:
								a.settingsAbout(c)
							default:
								a.settingsGeneral(c)
							}
						})
					})
				})
			})
		})
	})
}

// settingsHeading is a page's title. The page explains itself by its
// layout, so no line of description follows it.
func settingsHeading(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(24).FontWeight(600).Padding(0, 0, 8, 0)
}

// settingsSection is the title above a group of rows.
func settingsSection(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(15).FontWeight(600).Padding(16, 0, 0, 0)
}

// settingsGroup holds a section's rows. It draws no frame: the settings pane
// is the only surface, and whitespace separates the groups.
func settingsGroup(c *ui.Context, key string, rows func()) {
	ui.Column(c.Key(key)).Clip().Children(rows)
}

// settingsSeparator once drew a hairline between rows; rows are now kept
// apart by their own padding, so it draws nothing.
func settingsSeparator(c *ui.Context) {}

// settingsBadge renders a restrained pill badge for status or metadata.
func settingsBadge(c *ui.Context, text, tone string) {
	t := c.Theme()
	bg, ink := t.SurfaceHover, t.TextMuted
	switch tone {
	case "accent":
		bg, ink = t.Accent.Alpha(0.12), t.Accent
	case "danger":
		bg, ink = t.Danger.Alpha(0.12), t.Danger
	case "success":
		bg, ink = t.Success.Alpha(0.12), t.Success
	}
	ui.Box(c).Padding(4, 8).Radius(999).Background(bg).Shrink(0).Children(func() {
		ui.Text(c, text).FontSize(12).FontWeight(500).TextColor(ink).NoWrap()
	})
}

// settingsBanner renders a flat full-width status strip with an optional
// trailing action: a tint of its tone, no outline.
func settingsBanner(c *ui.Context, key, text, tone string, action func()) {
	t := c.Theme()
	bg, ink := t.SurfaceHover, t.Text
	switch tone {
	case "accent":
		bg = t.Accent.Alpha(0.08)
	case "danger":
		bg, ink = t.Danger.Alpha(0.08), t.Danger
	case "success":
		bg = t.Success.Alpha(0.08)
	}
	ui.Row(c.Key(key)).WidthPercent(100).Padding(12, 16).Gap(12).Radius(controlRadius).
		Background(bg).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(13).TextColor(ink).LineHeight(1.5).Grow(1).MinWidth(0)
		if action != nil {
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(action)
		}
	})
}

func settingsRowInset(c *ui.Context) float32 {
	_, h := c.Size()
	if h < 560 {
		return 8
	}
	return 12
}

// settingRow is a group's row: its label, and its control at the end, as
// every settings row lines up. note, when given, explains the row on hover
// and to assistive technology rather than as text under the label.
func settingRow(c *ui.Context, key, label, note string, control func()) {
	row := settingRowBase(c, key, label, "", control)
	if note != "" {
		row.Tooltip(note).Description(note)
	}
}

// settingDetailRow is settingRow with a line under the label that stays in
// view: data the row shows (a path, an account, a status) or a warning the
// user must see before acting.
func settingDetailRow(c *ui.Context, key, label, detail string, control func()) {
	settingRowBase(c, key, label, detail, control)
}

func settingRowBase(c *ui.Context, key, label, detail string, control func()) ui.Element {
	t := c.Theme()
	row := ui.Row(c.Key(key)).Padding(settingsRowInset(c), 0).Gap(24).AlignItems(ui.Center).MinHeight(52)
	row.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, label).FontSize(14)
			if detail != "" {
				ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted).LineHeight(1.5)
			}
		})
		ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
	})
	return row
}

// folderRow is a labeled folder with a button that opens it.
func folderRow(c *ui.Context, key, label, dir string) {
	settingDetailRow(c, key, label, shortPath(dir), func() {
		if ui.Button(c.Key("open"), i18n.T("在文件管理器中显示")).Tooltip(dir).Clicked() {
			os.MkdirAll(dir, 0o755)
			revealDir(dir)
		}
	})
}

func (a *App) settingsGeneral(c *ui.Context) {
	settingsHeading(c, i18n.T("通用"))
	settingsSection(c, i18n.T("位置"))
	settingsGroup(c, "places", func() {
		folderRow(c, "documents", i18n.T("新空间的默认位置"), a.cfg.DocumentsDir)
		settingsSeparator(c)
		folderRow(c, "config", i18n.T("配置文件夹（spaces.json、插件）"), a.cfg.ConfigDir)
	})
	settingsSection(c, i18n.T("窗口"))
	settingsGroup(c, "closing", func() {
		tray := !a.prefs.KeepRunningOff
		toggleRow(c, "close-to-tray", i18n.T("关闭窗口时最小化到托盘"), i18n.T("FreeFolio 继续在通知区域运行；点击托盘图标打开窗口，右键选择退出"), &tray, nil)
		if tray == a.prefs.KeepRunningOff {
			a.prefs.KeepRunningOff = !tray
			a.savePrefs()
			a.syncTray()
		}
		settingsSeparator(c)
		at := 0
		if a.prefs.StartAtHome {
			at = 1
		}
		settingRow(c, "start-at", i18n.T("打开 FreeFolio 时"), "", func() {
			segmentedChoice(c.Key("choice"), &at, i18n.T("继续上次阅读"), i18n.T("空间主页")).Label(i18n.T("打开 FreeFolio 时"))
			if home := at == 1; a.prefs.StartAtHome != home {
				a.prefs.StartAtHome = home
				a.savePrefs()
			}
		})
	})
	a.settingsQuickNote(c)
}

func (a *App) settingsWriting(c *ui.Context) {
	settingsHeading(c, i18n.T("写作"))
	settingsSection(c, i18n.T("编辑"))
	settingsGroup(c, "writing", func() {
		toggleRow(c, "typewriter", i18n.T("打字机滚动"), i18n.T("正在写的一行始终停在页面中间"), &a.prefs.Typewriter, a.savePrefs)
		settingsSeparator(c)
		autosave := !a.prefs.NoAutosave
		toggleRow(c, "autosave", i18n.T("自动保存"), fmt.Sprintf(i18n.T("编辑后自动写入文件；关闭后需按 %s 手动保存"), a.keyLabel("save")), &autosave, nil)
		if autosave == a.prefs.NoAutosave {
			a.toggleAutosave()
		}
		settingsSeparator(c)
		pair := !a.prefs.NoAutoPair
		toggleRow(c, "auto-pair", i18n.T("括号与引号自动配对"), "", &pair, nil)
		if pair == a.prefs.NoAutoPair {
			a.prefs.NoAutoPair = !pair
			a.savePrefs()
		}
		settingsSeparator(c)
		toggleRow(c, "auto-space", i18n.T("中英文之间自动加空格"), i18n.T("输入时在汉字与英文字母、数字之间加一个空格；代码与公式不受影响"), &a.prefs.AutoSpace, a.savePrefs)
		settingsSeparator(c)
		codeWrap := !a.prefs.CodeNoWrap
		toggleRow(c, "code-wrap", i18n.T("代码块自动换行"), i18n.T("关闭后长代码行横向滚动，也可在单个代码块右上角单独切换"), &codeWrap, nil)
		if codeWrap == a.prefs.CodeNoWrap {
			a.prefs.CodeNoWrap = !codeWrap
			a.savePrefs()
		}
		settingsSeparator(c)
		wideTables := !a.prefs.NoWideTables
		toggleRow(c, "wide-tables", i18n.T("宽表格延展到页边"), i18n.T("内容较宽的表格自动加宽，超出正文栏；关闭后表格始终保持正文宽度"), &wideTables, nil)
		if wideTables == a.prefs.NoWideTables {
			a.prefs.NoWideTables = !wideTables
			a.savePrefs()
		}
	})
	settingsSection(c, i18n.T("阅读"))
	settingsGroup(c, "reading", func() {
		dim := !a.prefs.NoDim
		toggleRow(c, "dim", i18n.T("专注模式淡化其他段落"), i18n.T("专注模式（")+a.keyLabel("focus")+i18n.T("）下只突出光标所在的段落"), &dim, nil)
		if dim == a.prefs.NoDim {
			a.toggleDim()
		}
		settingsSeparator(c)
		toggleRow(c, "draw-annotations", i18n.T("打开文档时画出手绘标注"), i18n.T("页面上的标注依次画出来；减弱动态效果时直接显示"), &a.prefs.DrawAnnotations, a.savePrefs)
	})
}

// settingsFiles are the choices of the file browser, where loose files open
// in tabs outside the spaces.
func (a *App) settingsFiles(c *ui.Context) {
	f := &a.prefs.Files
	settingsHeading(c, i18n.T("文件浏览"))
	settingsSection(c, i18n.T("标签页"))
	settingsGroup(c, "files-tabs", func() {
		show := !f.HideTabClose
		toggleRow(c, "tab-close", i18n.T("显示标签页关闭按钮"), i18n.T("隐藏后仍可用双击、鼠标中键或 ")+combo(ui.Cmd, ui.KeyW).Label()+i18n.T(" 关闭"), &show, nil)
		if show == f.HideTabClose {
			f.HideTabClose = !show
			a.savePrefs()
		}
		settingsSeparator(c)
		double := !f.NoDoubleClickClose
		toggleRow(c, "tab-double-click", i18n.T("双击标签页关闭"), "", &double, nil)
		if double == f.NoDoubleClickClose {
			f.NoDoubleClickClose = !double
			a.savePrefs()
		}
		settingsSeparator(c)
		at := 0
		if f.TabAfterCurrent {
			at = 1
		}
		settingRow(c, "tab-position", i18n.T("新标签页位置"), "", func() {
			segmentedChoice(c.Key("choice"), &at, i18n.T("末尾"), i18n.T("当前标签页右侧")).Label(i18n.T("新标签页位置"))
			if after := at == 1; after != f.TabAfterCurrent {
				f.TabAfterCurrent = after
				a.savePrefs()
			}
		})
	})
	settingsSection(c, i18n.T("打开与关闭"))
	settingsGroup(c, "files-window", func() {
		toggleRow(c, "files-read-only", i18n.T("以只读方式打开文件"), i18n.T("防止查看时误改；可在页面右上角的锁按钮恢复编辑"), &f.ReadOnly, a.savePrefs)
		settingsSeparator(c)
		remember := !f.NoRememberPlace
		toggleRow(c, "files-remember-place", i18n.T("打开时回到上次浏览的位置"), "", &remember, nil)
		if remember == f.NoRememberPlace {
			f.NoRememberPlace = !remember
			a.savePrefs()
		}
		settingsSeparator(c)
		quit := !f.KeepOpen
		toggleRow(c, "files-close-last", i18n.T("关闭最后一个标签页时关闭窗口"), "", &quit, nil)
		if quit == f.KeepOpen {
			f.KeepOpen = !quit
			a.savePrefs()
		}
	})
}

// settingsAppearance is how the window and pages look: the interface
// language and the sidebar's side, then the body's typeface, the width of
// the text column, the size and leading of body text, previewed in a
// small window drawn as FreeFolio's own.
func (a *App) settingsAppearance(c *ui.Context) {
	settingsHeading(c, i18n.T("外观"))
	a.appearancePreview(c)
	settingsSection(c, i18n.T("窗口"))
	settingsGroup(c, "window", func() {
		at := map[string]int{"": 0, "zh-Hans": 1, "en": 2}[a.prefs.Language]
		settingRow(c, "language", i18n.T("界面语言"), i18n.T("菜单栏在重新启动后切换"), func() {
			before := at
			segmentedChoice(c.Key("choice"), &at, i18n.T("跟随系统"), "简体中文", "English").Label(i18n.T("界面语言"))
			if at != before {
				a.prefs.Language = []string{"", "zh-Hans", "en"}[at]
				a.savePrefs()
				a.applyLanguage()
			}
		})
		settingsSeparator(c)
		side := 1
		if a.prefs.SidebarLeft {
			side = 0
		}
		settingRow(c, "sidebar-position", i18n.T("侧边栏位置"), "", func() {
			segmentedChoice(c.Key("choice"), &side, i18n.T("左侧"), i18n.T("右侧")).Label(i18n.T("侧边栏位置"))
			if left := side == 0; a.prefs.SidebarLeft != left {
				a.prefs.SidebarLeft = left
				a.savePrefs()
			}
		})
		settingsSeparator(c)
		toggleRow(c, "gray-folder-icons", i18n.T("侧边栏文件夹图标使用浅灰色"), "", &a.prefs.GrayFolderIcons, a.savePrefs)
	})
	settingsGroup(c, "sidebar-density", func() {
		settingRow(c, "density", i18n.T("侧边栏行高"), "", func() {
			at := slices.Index(prefs.SidebarDensities, a.prefs.SidebarDensity)
			before := at
			segmentedChoice(c, &at, i18n.T("紧凑"), i18n.T("标准"), i18n.T("舒适"))
			if at != before && at >= 0 {
				a.prefs.SidebarDensity = prefs.SidebarDensities[at]
				a.savePrefs()
			}
		})
	})
	settingsSection(c, i18n.T("文档"))
	settingsGroup(c, "appearance", func() {
		choice := func(key, label, note string, values []string, names []string, current *string, set func(string)) {
			at := 0
			for i, v := range values {
				if v == *current {
					at = i
				}
			}
			settingRow(c, key, label, note, func() {
				before := at
				segmentedChoice(c.Key("choice"), &at, names...).Label(label)
				if at != before && set != nil {
					set(values[at])
				} else if at != before {
					*current = values[at]
					a.savePrefs()
				}
			})
		}
		for _, forUI := range []bool{true, false} {
			label, value := i18n.T("正文字体"), a.prefs.BodyFont
			if forUI {
				label, value = i18n.T("界面字体"), a.prefs.InterfaceFont
			}
			if value == "" {
				value = i18n.T("跟随系统字体")
				if !forUI && a.prefs.Font != "" {
					value = a.prefs.Font
				}
			}
			settingRow(c, label, label, "", func() {
				if ui.Button(c.Key("choose-font"), value).MaxWidth(300).Clicked() {
					a.fonts.openFontPicker(forUI)
				}
			})
			if forUI {
				settingsSeparator(c)
			}
		}
		settingsSeparator(c)
		choice("page", i18n.T("页面宽度"), "", prefs.Pages, []string{i18n.T("窄"), i18n.T("标准"), i18n.T("宽"), i18n.T("全宽")}, &a.prefs.Page, a.setPageWidth)
		settingsSeparator(c)
		for i, item := range []struct {
			label string
			value float32
			step  float64
			size  bool
		}{{i18n.T("正文字号"), a.prefs.ReadingSize(), 1, true}, {i18n.T("行距"), a.prefs.ReadingLine(), .1, false}} {
			if i > 0 {
				settingsSeparator(c)
			}
			settingRow(c, item.label, item.label, "", func() {
				// The step stays a float64: as a float32, 0.1 widened to
				// 0.10000000149…, and the input showed that many decimals.
				value := float64(item.value)
				lo, hi := 12.0, 28.0
				if !item.size {
					lo, hi = 1.2, 2.2
					value = math.Round(value*10) / 10
				}
				ui.NumberInput(c.Key("number"), &value, lo, hi, item.step).Width(112).Label(item.label)
				if float32(value) != item.value {
					if item.size {
						a.prefs.FontSize = float32(value)
					} else {
						a.prefs.LineHeight = float32(math.Round(value*10) / 10)
					}
					a.savePrefs()
				}
			})
		}
	})
	if ui.Button(c.Key("appearance-reset"), i18n.T("恢复默认外观")).AlignSelf(ui.Start).Clicked() {
		a.prefs.FontSize, a.prefs.LineHeight, a.prefs.Font, a.prefs.Page = 0, 0, "", ""
		a.prefs.BodyFont, a.prefs.InterfaceFont = "", ""
		a.prefs.SidebarLeft = false
		a.prefs.GrayFolderIcons = false
		a.savePrefs()
	}
}

// appearancePreview is a small FreeFolio window drawn with the choices made
// below it: the sidebar on its side, and a page set by the real editor in
// the chosen typeface, size and leading, its column narrowed as the
// window's would be.
func (a *App) appearancePreview(c *ui.Context) {
	t := c.Theme()
	title := i18n.T("留一点空白")
	if a.sampleEditor == nil || a.sampleTitle != title {
		a.sampleTitle = title
		a.sampleEditor = editor.New("# " + title + "\n\n" + i18n.T("清晨打开窗，树影落在桌角；翻到新的一页，把今天想记住的事写下来") +
			"\n\n- " + i18n.T("读完一本书的最后一章") + "\n- " + i18n.T("整理散落在笔记里的灵感") +
			"\n\n> " + i18n.T("不必急着写完，先把想说的话写下来"))
		a.sampleEditor.SetReadOnly(true)
	}
	paper := documentTheme(a.paperDark)
	column := a.prefs.Column()
	if column == 0 {
		column = 820
	}
	// The preview is laid out as the window is: the sidebar runs the full
	// height with its toggle, history arrows and search at its top. The
	// brand leads the window, in the left sidebar's header or the page's
	// toolbar, with the document's name and space above the page.
	frame := ui.Row(c.Key("appearance-sample")).Height(300).Radius(panelRadius).Border(1, t.Border).
		Background(t.Background).Clip().Label(i18n.T("外观预览")).
		Padding(0, 12, 12, 12).Gap(12).AlignItems(ui.Stretch)
	if !a.prefs.SidebarLeft {
		frame.Reverse()
	}
	icon := func(svg *ui.SVG) { ui.Icon(c, svg).FontSize(10).TextColor(t.TextMuted) }
	previewBrand := func() {
		ui.Row(c.Key("preview-brand")).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Image(c, brandLogo()).Size(14, 14)
			ui.Text(c, "FreeFolio").FontSize(10).FontWeight(600)
		})
	}
	frame.Children(func() {
		ui.Column(c.Key("preview-side")).Width(136).Shrink(0).Gap(8).Children(func() {
			height := float32(26)
			if a.prefs.SidebarLeft {
				height = 30
			}
			ui.Row(c).Height(height).AlignItems(ui.Center).Children(func() {
				if a.prefs.SidebarLeft {
					previewBrand()
					ui.Spacer(c)
				}
				icon(a.sidebarIcon(false))
				if !a.prefs.SidebarLeft {
					ui.Spacer(c)
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				icon(iconBack)
				icon(iconForward)
				icon(iconReload)
				ui.Row(c).Grow(1).MinWidth(0).Height(20).Radius(controlRadius).Background(t.Surface).PaddingX(8).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconSearch).FontSize(10).TextColor(t.TextMuted)
					ui.Text(c, i18n.T("搜索")).FontSize(10).TextColor(t.TextMuted).SingleLine()
				})
			})
			for i, name := range []string{title, i18n.T("读书笔记"), i18n.T("旅行计划")} {
				row := ui.Row(c.Key(fmt.Sprintf("preview-doc-%d", i))).Height(22).Radius(controlRadius).PaddingX(8).Gap(8).AlignItems(ui.Center)
				ink := t.Text
				if i == 0 {
					row.Background(t.Accent.Alpha(0.12))
					ink = t.Accent
				}
				row.Children(func() {
					if i == 1 {
						ui.Icon(c, iconFolder).FontSize(10).TextColor(a.sidebarFolderIconColor(t))
					} else {
						ui.Icon(c, iconFile).FontSize(10).TextColor(ink)
					}
					ui.Text(c, name).FontSize(10).TextColor(ink).SingleLine()
				})
			}
		})
		ui.Column(c.Key("preview-main")).Grow(1).MinWidth(0).Children(func() {
			bar := ui.Row(c.Key("preview-bar")).Height(30).Shrink(0).Gap(8).AlignItems(ui.Center)
			bar.Children(func() {
				caption := func() {
					ui.Column(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
						ui.Text(c, title+".md").FontSize(11).FontWeight(600).SingleLine()
						if sp := a.state.ActiveSpace(); sp != nil {
							ui.Text(c, sp.Name).FontSize(10).TextColor(t.TextMuted).SingleLine()
						}
					})
				}
				if a.prefs.SidebarLeft {
					start := ui.Row(c.Key("preview-start")).Width(0).Shrink(0)
					end := ui.Row(c.Key("preview-end")).Gap(8).Shrink(0).AlignItems(ui.Center).MarginStart(ui.Auto).Children(func() {
						icon(iconAsk)
						icon(iconEllipsis)
					})
					centeredTitle(c, bar, start, end, caption)
					return
				}
				previewBrand()
				caption()
				icon(iconAsk)
				icon(iconEllipsis)
			})
			sheet := ui.Themed(c.Key("preview-paper"), paper).Grow(1).MinHeight(0).Radius(8).Background(paper.Background).
				Border(1, paper.Border).Clip().Disabled(true).DimDisabled(false)
			w := sheet.LayoutBounds().W
			if w <= 0 {
				w = 460 // replaced by the measured width on the next frame
				c.AnimationFrame()
			}
			// The page's column as it is in a window about 1100 wide.
			col := max(160, column*w/1100)
			if column < 0 {
				col = editor.FullColumn
			}
			sheet.Children(func() {
				editor.View(c.Key("sample-text"), a.sampleEditor, editor.Options{
					Width: w - 2, Theme: paper, Page: pageStyle(a.paperDark), Column: col,
					FontSize: a.prefs.ReadingSize(), LineHeight: a.prefs.ReadingLine(), FontFamily: a.fonts.bodyFont(),
				})
			})
		})
	})
}

// toggleRow is a labeled switch; changed runs when it is flipped.
func toggleRow(c *ui.Context, key, label, note string, on *bool, changed func()) {
	settingRow(c, key, label, note, func() {
		before := *on
		ui.Switch(c.Key("switch"), on).Label(label)
		if *on != before && changed != nil {
			changed()
		}
	})
}

func (a *App) settingsSpaces(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.End).Gap(20).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			settingsHeading(c, i18n.T("空间"))
			// Why the space controls are off stays in view.
			if a.files != nil {
				ui.Text(c, i18n.T("返回空间后才能新建、编辑或移除空间")).FontSize(13).TextColor(t.TextMuted)
			}
		})
		if ui.Button(c.Key("settings-new-space"), i18n.T("新建空间")).Disabled(a.files != nil).Clicked() {
			a.settingsOpen = false
			a.newSpaceForm()
		}
	})
	active := a.state.ActiveSpace()
	ui.Scroll(c.Key("space-list")).Grow(1).MinHeight(0).Gap(20).Children(func() {
		settingsSection(c, i18n.T("我的空间"))
		settingsGroup(c, "spaces", func() {
			for i, s := range a.state.Spaces {
				if i > 0 {
					settingsSeparator(c)
				}
				accent := spaceAccent(space.ThemeOf(s).Colors[0], t.Dark)
				spaceRow := ui.Row(c.Key("space-row-"+s.ID)).Padding(12, 0).Gap(16).AlignItems(ui.Center)
				spaceRow.Children(func() {
					ui.Box(c).Size(32, 32).Center().Shrink(0).Children(func() {
						a.spaceGlyph(c, s.Icon, s.Symbol, s.Image, s.Name, 18, accent)
					})
					ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(4).Children(func() {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, s.Name).FontSize(14).SingleLine()
							if active != nil && active.ID == s.ID {
								ui.Row(c).Padding(4, 8).Radius(8).Background(accent.Alpha(0.12)).AlignItems(ui.Center).Shrink(0).Children(func() {
									ui.Text(c, i18n.T("当前")).FontSize(10.5).FontWeight(600).TextColor(accent).SingleLine()
								})
							}
						})
						if !s.IsExternal() {
							ui.Text(c, shortPath(s.Root)).FontSize(13).TextColor(t.TextMuted).SingleLine().Tooltip(s.Root)
						}
					})
					actions := ui.Row(c.Key("actions")).Shrink(0).Gap(8).AlignItems(ui.Center)
					revealOnIntent(spaceRow, actions)
					actions.Children(func() {
						if ui.Button(c.Key("edit"), i18n.T("编辑")).Label(i18n.T("编辑 ") + s.Name).Disabled(a.files != nil).Clicked() {
							a.settingsOpen = false
							a.editSpace(s)
						}
						if ui.Button(c.Key("reveal"), i18n.T("显示")).Label(i18n.T("在文件管理器中显示 ") + s.Name).Disabled(s.IsExternal()).Clicked() {
							revealDir(s.Root)
						}
						if ui.Button(c.Key("remove"), i18n.T("移除")).Label(i18n.T("从 FreeFolio 移除 ") + s.Name).Disabled(s.IsExternal() || len(a.state.Spaces) < 2 || a.files != nil).
							Tooltip(i18n.T("只删除记录，文件夹保留")).Clicked() {
							a.removeSpace(s.ID)
						}
					})
				})
			}
		})
		settingsSection(c, i18n.T("空间工具"))
		settingsGroup(c, "space-tools", func() {
			settingRow(c, "space-tool-import", i18n.T("导入已有笔记"), i18n.T("从 Obsidian、Logseq、Typora、iA Writer 或思源复制导入，原文件不变"), func() {
				if ui.Button(c.Key("import-existing-notes"), i18n.T("导入")).Label(i18n.T("导入已有笔记")).Clicked() {
					a.openImportGuide()
				}
			})
			cleanup := func() {
				if ui.Button(c.Key("scan-unused-assets"), i18n.T("清理")).Label(i18n.T("清理未使用附件")).Disabled(active == nil || active.IsExternal()).Clicked() {
					a.scanUnusedAssets()
				}
			}
			// Why the button is off stays in view; what it does is a hover hint.
			if active == nil || active.IsExternal() {
				settingDetailRow(c, "space-tool-cleanup", i18n.T("清理未使用附件"), i18n.T("切换到普通空间后可检查未引用附件"), cleanup)
			} else {
				settingRow(c, "space-tool-cleanup", i18n.T("清理未使用附件"), i18n.T("扫描当前空间的未引用附件，备份后移到系统回收站"), cleanup)
			}
		})
		settingsSection(c, i18n.T("新建默认"))
		settingsGroup(c, "space-defaults", func() {
			starter := !a.prefs.NoSpaceStarter
			toggleRow(c, "space-starter", i18n.T("新建空白空间时从模板开始"), "", &starter, nil)
			if starter == a.prefs.NoSpaceStarter {
				a.prefs.NoSpaceStarter = !starter
				a.savePrefs()
			}
		})
	})
}

// settingsUpdates shows the current version, update status, release notes and
// automatic download preference.
func (a *App) settingsUpdates(c *ui.Context) {
	settingsHeading(c, i18n.T("更新"))
	a.updates.updateOverview(c)
	a.updates.updateReleaseNotes(c)
	settingsSection(c, i18n.T("更新偏好"))
	settingsGroup(c, "update-prefs", func() {
		auto := !a.prefs.NoAutoUpdate
		toggleRow(c, "auto-update", i18n.T("自动更新 FreeFolio"), i18n.T("在后台下载新版本，准备好后由你安装；关闭后只提示有新版本"), &auto, nil)
		if auto == a.prefs.NoAutoUpdate {
			a.updates.toggleAutoUpdate()
		}
	})
}

func (a *App) settingsAbout(c *ui.Context) {
	t := c.Theme()
	ui.Row(c.Key("about-product")).Gap(20).AlignItems(ui.Center).Padding(0, 0, 8, 0).Children(func() {
		ui.Image(c, brandLogo()).Size(48, 48).Shrink(0)
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, "FreeFolio").FontSize(20).FontWeight(600)
			ui.Text(c, i18n.T("版本 ")+Version+" · "+i18n.T("安静地写作，有序地整理")).FontSize(13).TextColor(t.TextMuted)
		})
	})
	settingsGroup(c, "about-intro", func() {
		settingDetailRow(c, "about-github", i18n.T("开源仓库"), "github.com/OUBIGFA/FreeFolio", func() {
			ui.Link(c.Key("repository"), i18n.T("打开"), "https://github.com/OUBIGFA/FreeFolio").FontSize(13).
				Label(i18n.T("GitHub 开源仓库")).Tooltip(i18n.T("在浏览器中打开 GitHub 开源仓库"))
		})
		settingsSeparator(c)
		settingDetailRow(c, "about-icons", i18n.T("图标"), i18n.T("Solar Icons，作者 480 Design，采用 CC BY 4.0 许可"), func() {
			ui.Link(c.Key("solar-icons"), i18n.T("打开"), "https://github.com/saoudi-h/solar-icons").FontSize(13).
				Label(i18n.T("Solar Icons 项目")).Tooltip(i18n.T("在浏览器中打开 Solar Icons 项目"))
		})
		settingsSeparator(c)
		settingRow(c, "about-tour", i18n.T("新手教程"), "", func() {
			if ui.Button(c.Key("replay-tour"), i18n.T("重新观看")).Disabled(a.files != nil).Clicked() {
				a.startTour()
			}
		})
	})
}
