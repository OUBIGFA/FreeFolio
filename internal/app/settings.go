package app

import (
	"fmt"
	"math"
	"os"

	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/prefs"
	"github.com/OUBIGFA/FreeMD/internal/space"
	"github.com/egoist/mygo/ui"
)

// Settings tabs.
const (
	tabCloud      = "cloud-sync"
	tabGeneral    = "general"
	tabAppearance = "appearance"
	tabSpaces     = "spaces"
	tabShortcuts  = "shortcuts"
	tabAI         = "ai"
	tabWriting    = "writing"
	tabFiles      = "files"
	tabUpdates    = "updates"
	tabAbout      = "about"
)

// openSettings shows the settings on a tab.
func (a *App) openSettings(tab string) {
	a.settingsOpen = true
	a.settingsTab = tab
}

// moreMenu keeps document actions at hand. Library and settings lead to
// app-wide tools without repeating their individual pages here.
func (a *App) moreMenu(c *ui.Context) {
	b := iconButton(c, "more", iconEllipsis, i18n.T("更多"), i18n.T("更多"), false).Role(ui.RoleMenuButton)
	b.PopoverMenuButton(func(m *ui.Menu) {
		noDoc := a.doc == nil || a.doc.loadErr != nil
		if a.menuKey(m.Item(i18n.T("专注模式")), "focus").Checked(a.focus).Chosen() {
			a.toggleFocus()
		}
		if a.menuKey(m.Item(i18n.T("目录")), "outline").Checked(a.outlineOpen()).Disabled(noDoc || a.doc.source).Chosen() {
			a.toggleOutline()
		}
		mode := i18n.T("预览 Markdown 导出")
		if a.doc != nil && a.doc.source {
			mode = i18n.T("返回富文本编辑")
		}
		if a.menuKey(m.Item(mode), "source").Disabled(noDoc).Chosen() {
			a.toggleSource()
		}
		if a.menuKey(m.Item(i18n.T("添加分屏")), "split-add").Disabled(a.doc == nil).Chosen() {
			a.addSplitPane()
		}
		m.Separator()
		if a.menuKey(m.Item(i18n.T("版本历史…")), "history").Disabled(noDoc).Chosen() {
			a.openVersions()
		}
		m.Submenu(i18n.T("导出"), func(sub *ui.Menu) {
			if sub.Item(i18n.T("导出 Markdown 文件")).Disabled(noDoc).Chosen() {
				a.exportMarkdown()
			}
			if a.menuKey(sub.Item(i18n.T("导出 PDF")), "export-pdf").Disabled(noDoc).Chosen() {
				a.exportPDF()
			}
			if sub.Item(i18n.T("导出 HTML 网页")).Disabled(noDoc).Chosen() {
				a.exportHTML()
			}
		}).Disabled(noDoc)
		if m.Item(i18n.T("打印…")).Disabled(noDoc).Chosen() {
			a.printDoc()
		}
		if m.Item(i18n.T("在文件管理器中显示文档")).Disabled(noDoc).Chosen() {
			a.revealIn(a.doc.space, a.doc.rel)
		}
		m.Separator()
		if m.Item(i18n.T("打开文件…")).Chosen() {
			a.chooseExternalFiles()
		}
		if m.Item(i18n.T("另存为…")).Disabled(noDoc).Chosen() {
			a.chooseSaveAs()
		}
		if a.menuKey(m.Item(i18n.T("资料库")), "library").Chosen() {
			a.toggleLibrary()
		}
		if a.menuKey(m.Item(i18n.T("设置…")), "settings").Chosen() {
			a.openSettings(tabGeneral)
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
		{tabAI, i18n.T("AI 问答"), "ask"},
		{tabCloud, i18n.T("云端同步"), "cloud"},
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
	t := c.Theme()
	w, h := c.Size()
	bar := c.TitleBar()
	rail := float32(212)
	if w < 800 {
		rail = 148
	}
	ui.Overlay(c.Key("settings"), func() {
		ui.Column(c.Key("settings-surface")).Fill().Modal().Role(ui.RoleDialog).Label(i18n.T("设置")).Background(t.Background).Children(func() {
			ui.Box(c.Key("settings-title")).Height(max(12, bar.Height)).DragWindow()
			ui.Row(c.Key("settings-body")).Grow(1).MinHeight(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c.Key("settings-nav")).Width(rail).Shrink(0).Padding(0, 10, 10, 10).Gap(8).Children(func() {
					// The way back heads the navigation, beside its title.
					ui.Row(c.Key("settings-back-row")).Gap(4).AlignItems(ui.Center).Children(func() {
						if iconButton(c, "settings-back", iconBack, i18n.T("返回"), i18n.T("返回")+" (Esc)", false).Clicked() {
							a.settingsOpen = false
						}
						ui.Text(c, i18n.T("设置")).FontSize(15).FontWeight(600)
					})
					ui.Sidebar(c.Key("settings-tabs"), &a.settingsTab, func() {
						for _, tab := range settingsTabs() {
							ui.SidebarItem(c, tab.id, settingsIcons[tab.icon], tab.label).Height(min(32, h/14))
						}
					}).Label(i18n.T("设置")).Grow(1).Shrink(1).MinHeight(0).Padding(0).Gap(2).Background(ui.Transparent)
					if a.recording == "" && c.Shortcut(0, ui.KeyEscape) {
						a.settingsOpen = false
					}
				})
				pane := ui.Column(c.Key("settings-pane")).Grow(1).MinWidth(0).Background(t.Surface).Border(1, t.Border).Radius(panelRadius).Margin(0, 8, 8, 0).Clip()
				pane.Children(func() {
					ui.Row(c.Key("settings-header")).Height(44).Shrink(0).PaddingX(20).AlignItems(ui.Center).Children(func() {
						chosen, current := 1, ""
						for _, tab := range settingsTabs() {
							if tab.id == a.settingsTab {
								current = tab.label
							}
						}
						if ui.Breadcrumbs(c.Key("crumbs"), []string{i18n.T("设置"), current}, &chosen).Label(i18n.T("设置")).Changed() && chosen == 0 {
							a.settingsTab = tabGeneral
						}
					})
					ui.Box(c).Height(1).Shrink(0).Background(t.Border)
					container := ui.Scroll
					if a.settingsTab == tabSpaces || a.settingsTab == tabShortcuts {
						container = ui.Column
					}
					pad := min(32, h/24)
					container(c.Key("content")).Grow(1).MinHeight(0).Padding(pad, min(32, w/30)).AlignItems(ui.Center).Children(func() {
						ui.Column(c.Key("settings-page-" + a.settingsTab)).Width(max(0, min(680, w-rail-10-2*min(32, w/30)))).Grow(1).MinHeight(0).Gap(min(14, h/50)).Children(func() {
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
								a.settingsCloudSync(c)
							case tabAI:
								a.settingsAI(c)
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

// settingsHeading is a page's title and what it is for.
func settingsHeading(c *ui.Context, title, note string) {
	t := c.Theme()
	ui.Column(c).Gap(4).Padding(0, 0, 4, 0).Children(func() {
		ui.Text(c, title).FontSize(20).FontWeight(600)
		_, h := c.Size()
		if note != "" && h >= 580 {
			ui.Text(c, note).FontSize(13).TextColor(t.TextMuted).LineHeight(1.5)
		}
	})
}

// settingsSection is the title above a group of rows.
func settingsSection(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(14).FontWeight(600).Padding(6, 2, 0, 2)
}

// Settings groups share a surface and a common row inset.
func settingsGroup(c *ui.Context, key string, rows func()) {
	t := c.Theme()
	ui.Column(c.Key(key)).Background(t.Surface).Border(1, t.Border).Radius(panelRadius).Clip().Children(rows)
}

// settingsSeparator is the hairline between a group's rows, inset as they are.
func settingsSeparator(c *ui.Context) {
	ui.Box(c).Height(1).Margin(0, 16).Background(c.Theme().Border.Alpha(0.6))
}

func settingsRowInset(c *ui.Context) float32 {
	_, h := c.Size()
	if h < 560 {
		return 6
	}
	return min(12, (h-450)/24)
}

// settingRow is a group's row: its label and note, and its control at
// the end, as every settings row lines up.
func settingRow(c *ui.Context, key, label, note string, control func()) {
	t := c.Theme()
	_, h := c.Size()
	ui.Row(c.Key(key)).Padding(settingsRowInset(c), 16).Gap(16).AlignItems(ui.Center).MinHeight(48).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(500)
			if note != "" && h >= 560 {
				ui.Text(c, note).FontSize(12).TextColor(t.TextMuted).LineHeight(1.45)
			}
		})
		ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
	})
}

// folderRow is a labeled folder with a button that opens it.
func folderRow(c *ui.Context, key, label, dir string) {
	settingRow(c, key, label, shortPath(dir), func() {
		if ui.Button(c.Key("open"), i18n.T("在文件管理器中显示")).Tooltip(dir).Clicked() {
			os.MkdirAll(dir, 0o755)
			revealDir(dir)
		}
	})
}

func (a *App) settingsGeneral(c *ui.Context) {
	t := c.Theme()
	settingsHeading(c, i18n.T("通用"), i18n.T("FreeMD 把文档保存在你的文件夹里，设置与空间列表保存在配置文件夹。"))
	settingsSection(c, i18n.T("位置"))
	settingsGroup(c, "places", func() {
		folderRow(c, "documents", i18n.T("新空间的默认位置"), a.cfg.DocumentsDir)
		settingsSeparator(c)
		folderRow(c, "config", i18n.T("配置文件夹（spaces.json、插件）"), a.cfg.ConfigDir)
	})
	ui.Text(c, i18n.T("颜色与图标按空间设置：在侧边栏的空间名称上右键，选择要修改的属性。")).
		FontSize(12).TextColor(t.TextMuted).Padding(0, 2)
	settingsSection(c, i18n.T("窗口"))
	settingsGroup(c, "closing", func() {
		tray := !a.prefs.KeepRunningOff
		toggleRow(c, "close-to-tray", i18n.T("关闭窗口时最小化到托盘"), i18n.T("FreeMD 继续在通知区域运行；点击托盘图标打开窗口，右键选择退出。"), &tray, nil)
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
		settingRow(c, "start-at", i18n.T("打开 FreeMD 时"), i18n.T("启动或从托盘重新打开窗口时显示的内容。"), func() {
			segmentedChoice(c.Key("choice"), &at, i18n.T("继续上次阅读"), i18n.T("空间主页")).Label(i18n.T("打开 FreeMD 时"))
			if home := at == 1; a.prefs.StartAtHome != home {
				a.prefs.StartAtHome = home
				a.savePrefs()
			}
		})
	})
}

func (a *App) settingsWriting(c *ui.Context) {
	settingsHeading(c, i18n.T("写作"), i18n.T("编辑时的辅助行为，对所有文档生效。"))
	settingsSection(c, i18n.T("编辑"))
	settingsGroup(c, "writing", func() {
		toggleRow(c, "typewriter", i18n.T("打字机滚动"), i18n.T("正在写的一行始终停在页面中间。"), &a.prefs.Typewriter, a.savePrefs)
		settingsSeparator(c)
		pair := !a.prefs.NoAutoPair
		toggleRow(c, "auto-pair", i18n.T("括号与引号自动配对"), i18n.T("支持中文符号、包裹选区和成对删除。"), &pair, nil)
		if pair == a.prefs.NoAutoPair {
			a.prefs.NoAutoPair = !pair
			a.savePrefs()
		}
		settingsSeparator(c)
		toggleRow(c, "auto-space", i18n.T("中英文之间自动加空格"), i18n.T("输入时在汉字与英文字母、数字之间加一个空格；代码与公式不受影响。"), &a.prefs.AutoSpace, a.savePrefs)
		settingsSeparator(c)
		codeWrap := !a.prefs.CodeNoWrap
		toggleRow(c, "code-wrap", i18n.T("代码块自动换行"), i18n.T("关闭后长代码行横向滚动，也可在单个代码块右上角单独切换。"), &codeWrap, nil)
		if codeWrap == a.prefs.CodeNoWrap {
			a.prefs.CodeNoWrap = !codeWrap
			a.savePrefs()
		}
	})
	settingsSection(c, i18n.T("阅读"))
	settingsGroup(c, "reading", func() {
		dim := !a.prefs.NoDim
		toggleRow(c, "dim", i18n.T("专注模式淡化其他段落"), i18n.T("专注模式（")+a.keyLabel("focus")+i18n.T("）下只突出光标所在的段落。"), &dim, nil)
		if dim == a.prefs.NoDim {
			a.toggleDim()
		}
		settingsSeparator(c)
		toggleRow(c, "draw-annotations", i18n.T("打开文档时画出手绘标注"), i18n.T("页面上的标注依次画出来；减弱动态效果时直接显示。"), &a.prefs.DrawAnnotations, a.savePrefs)
	})
}

// settingsFiles are the choices of the file browser, where loose files open
// in tabs outside the spaces.
func (a *App) settingsFiles(c *ui.Context) {
	f := &a.prefs.Files
	settingsHeading(c, i18n.T("文件浏览"), i18n.T("单独打开的文件显示在文件浏览的标签页中，用完即关；只有「返回空间」会回到空间。"))
	settingsSection(c, i18n.T("标签页"))
	settingsGroup(c, "files-tabs", func() {
		show := !f.HideTabClose
		toggleRow(c, "tab-close", i18n.T("显示标签页关闭按钮"), i18n.T("隐藏后仍可用双击、鼠标中键或 ")+combo(ui.Cmd, ui.KeyW).Label()+i18n.T(" 关闭。"), &show, nil)
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
		toggleRow(c, "files-read-only", i18n.T("以只读方式打开文件"), i18n.T("防止查看时误改；可在页面右上角的锁按钮恢复编辑。"), &f.ReadOnly, a.savePrefs)
		settingsSeparator(c)
		quit := !f.KeepOpen
		toggleRow(c, "files-close-last", i18n.T("关闭最后一个标签页时关闭窗口"), i18n.T("关闭此项后保留空白的文件浏览页，可以继续打开文件。"), &quit, nil)
		if quit == f.KeepOpen {
			f.KeepOpen = !quit
			a.savePrefs()
		}
	})
}

// settingsAppearance is how the window and pages look: the interface
// language and the sidebar's side, then the body's typeface, the width of
// the text column, the size and leading of body text, previewed in a
// small window drawn as FreeMD's own.
func (a *App) settingsAppearance(c *ui.Context) {
	settingsHeading(c, i18n.T("外观"), i18n.T("选择侧边栏的位置，调整所有文档共用的字体和版式。"))
	a.appearancePreview(c)
	settingsSection(c, i18n.T("窗口"))
	settingsGroup(c, "window", func() {
		at := map[string]int{"": 0, "zh-Hans": 1, "en": 2}[a.prefs.Language]
		settingRow(c, "language", i18n.T("界面语言"), i18n.T("菜单栏在重新启动后切换。"), func() {
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
		settingRow(c, "sidebar-position", i18n.T("侧边栏位置"), i18n.T("空间、收藏和文档列表所在的一侧。"), func() {
			segmentedChoice(c.Key("choice"), &side, i18n.T("左侧"), i18n.T("右侧")).Label(i18n.T("侧边栏位置"))
			if left := side == 0; a.prefs.SidebarLeft != left {
				a.prefs.SidebarLeft = left
				a.savePrefs()
			}
		})
	})
	settingsSection(c, i18n.T("文档"))
	settingsGroup(c, "appearance", func() {
		choice := func(key, label, note string, values []string, names []string, current *string) {
			at := 0
			for i, v := range values {
				if v == *current {
					at = i
				}
			}
			settingRow(c, key, label, note, func() {
				before := at
				segmentedChoice(c.Key("choice"), &at, names...).Label(label)
				if at != before {
					*current = values[at]
					a.savePrefs()
				}
			})
		}
		choice("font", i18n.T("正文字体"), "", prefs.Fonts, []string{i18n.T("系统"), i18n.T("衬线"), i18n.T("楷体"), i18n.T("等宽")}, &a.prefs.Font)
		settingsSeparator(c)
		choice("page", i18n.T("页面宽度"), "", prefs.Pages, []string{i18n.T("窄"), i18n.T("标准"), i18n.T("宽")}, &a.prefs.Page)
		settingsSeparator(c)
		for i, item := range []struct {
			label string
			value float32
			step  float32
			size  bool
		}{{i18n.T("正文字号"), a.prefs.ReadingSize(), 1, true}, {i18n.T("行距"), a.prefs.ReadingLine(), .1, false}} {
			if i > 0 {
				settingsSeparator(c)
			}
			settingRow(c, item.label, item.label, "", func() {
				value := float64(item.value)
				lo, hi := 12.0, 28.0
				if !item.size {
					lo, hi = 1.2, 2.2
				}
				ui.NumberInput(c.Key("number"), &value, lo, hi, float64(item.step)).Width(112).Label(item.label)
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
		a.prefs.SidebarLeft = false
		a.savePrefs()
	}
}

// appearancePreview is a small FreeMD window drawn with the choices made
// below it: the sidebar on its side, and a page set by the real editor in
// the chosen typeface, size and leading, its column narrowed as the
// window's would be.
func (a *App) appearancePreview(c *ui.Context) {
	t := c.Theme()
	title := i18n.T("留一点空白")
	if a.sampleEditor == nil || a.sampleTitle != title {
		a.sampleTitle = title
		a.sampleEditor = editor.New("# " + title + "\n\n" + i18n.T("清晨打开窗，树影落在桌角。翻到新的一页，把今天想记住的事写下来。") +
			"\n\n- " + i18n.T("读完一本书的最后一章") + "\n- " + i18n.T("整理散落在笔记里的灵感") +
			"\n\n> " + i18n.T("不必急着写完，先把想说的话写下来。"))
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
		Background(t.Background).Clip().Label(i18n.T("外观预览")).Shadow(0, 2, 8, 0, black.Alpha(0.06)).
		Padding(0, 8, 8, 8).Gap(8).AlignItems(ui.Stretch)
	if !a.prefs.SidebarLeft {
		frame.Reverse()
	}
	icon := func(svg *ui.SVG) { ui.Icon(c, svg).FontSize(10).TextColor(t.TextMuted) }
	previewBrand := func() {
		ui.Row(c.Key("preview-brand")).Gap(5).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Image(c, brandLogo).Size(14, 14)
			ui.Text(c, "FreeMD").FontSize(10).FontWeight(600)
		})
	}
	frame.Children(func() {
		ui.Column(c.Key("preview-side")).Width(136).Shrink(0).Gap(6).Children(func() {
			height := float32(26)
			if a.prefs.SidebarLeft {
				height = 30
			}
			ui.Row(c).Height(height).AlignItems(ui.Center).Children(func() {
				if a.prefs.SidebarLeft {
					previewBrand()
				}
				ui.Spacer(c)
				icon(a.sidebarIcon())
			})
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				icon(iconBack)
				icon(iconForward)
				icon(iconReload)
				ui.Row(c).Grow(1).MinWidth(0).Height(20).Radius(controlRadius).Border(1, t.Border).Background(t.Surface).PaddingX(5).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconSearch).FontSize(10).TextColor(t.TextMuted)
					ui.Text(c, i18n.T("搜索")).FontSize(10).TextColor(t.TextMuted).SingleLine()
				})
			})
			for i, name := range []string{title, i18n.T("读书笔记"), i18n.T("旅行计划")} {
				row := ui.Row(c.Key(fmt.Sprintf("preview-doc-%d", i))).Height(22).Radius(controlRadius).PaddingX(6).Gap(5).AlignItems(ui.Center)
				ink := t.Text
				if i == 0 {
					row.Background(t.Accent.Alpha(0.12))
					ink = t.Accent
				}
				row.Children(func() {
					ui.Icon(c, iconFile).FontSize(10).TextColor(ink)
					ui.Text(c, name).FontSize(10).TextColor(ink).SingleLine()
				})
			}
		})
		ui.Column(c.Key("preview-main")).Grow(1).MinWidth(0).Children(func() {
			bar := ui.Row(c.Key("preview-bar")).Height(30).Shrink(0).Gap(6).AlignItems(ui.Center)
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
					end := ui.Row(c.Key("preview-end")).Gap(6).Shrink(0).AlignItems(ui.Center).MarginStart(ui.Auto).Children(func() {
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
			sheet.Children(func() {
				editor.View(c.Key("sample-text"), a.sampleEditor, editor.Options{
					Width: w - 2, Theme: paper, Page: pageStyle(a.paperDark), Column: col,
					FontSize: a.prefs.ReadingSize(), LineHeight: a.prefs.ReadingLine(), FontFamily: a.prefs.BodyFamily(),
				})
			})
		})
	})
	ui.Text(c, fmt.Sprintf(i18n.T("%g 字号 · %.1f 倍行距"), a.prefs.ReadingSize(), a.prefs.ReadingLine())).
		FontSize(12).TextColor(t.TextMuted).Padding(0, 2)
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
	ui.Row(c).AlignItems(ui.End).Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			note := i18n.T("从 FreeMD 移除空间只删除记录，不会删除文件夹。")
			if a.files != nil {
				note = i18n.T("返回空间后才能新建、编辑或移除空间。")
			}
			settingsHeading(c, i18n.T("空间"), note)
		})
		if ui.Button(c.Key("settings-new-space"), i18n.T("新建空间…")).Disabled(a.files != nil).Clicked() {
			a.settingsOpen = false
			a.newSpaceForm()
		}
	})
	ui.Scroll(c.Key("space-list")).Grow(1).MinHeight(0).Children(func() {
		settingsGroup(c, "spaces", func() {
			for i, s := range a.state.Spaces {
				if i > 0 {
					settingsSeparator(c)
				}
				ui.Row(c.Key("space-row-"+s.ID)).Padding(10, 16).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(30, 30).Radius(controlRadius).Center().Background(spaceAccent(space.ThemeOf(s).Colors[0], t.Dark).Alpha(0.12)).Children(func() {
						a.spaceGlyph(c, s.Icon, s.Symbol, s.Image, s.Name, 15, spaceAccent(space.ThemeOf(s).Colors[0], t.Dark))
					})
					ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(2).Children(func() {
						ui.Text(c, s.Name).FontSize(13).FontWeight(500).SingleLine()
						if s.IsExternal() {
							ui.Text(c, i18n.T("内置空间 · 文件保留在原位置")).FontSize(12).TextColor(t.TextMuted).SingleLine()
						} else {
							ui.Text(c, shortPath(s.Root)).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(s.Root)
						}
					})
					if ui.Button(c.Key("edit"), i18n.T("编辑")).Label(i18n.T("编辑 ") + s.Name).Disabled(a.files != nil).Clicked() {
						a.settingsOpen = false
						a.editSpace(s)
					}
					if ui.Button(c.Key("reveal"), i18n.T("显示")).Label(i18n.T("在文件管理器中显示 ") + s.Name).Disabled(s.IsExternal()).Clicked() {
						revealDir(s.Root)
					}
					if ui.Button(c.Key("remove"), i18n.T("移除")).Label(i18n.T("从 FreeMD 移除 ") + s.Name).Disabled(s.IsExternal() || len(a.state.Spaces) < 2 || a.files != nil).
						Tooltip(i18n.T("只删除记录，文件夹保留")).Clicked() {
						a.removeSpace(s.ID)
					}
				})
			}
		})
	})
}

// settingsUpdates is whether FreeMD downloads new versions by itself, and
// the check of "检查更新…".
func (a *App) settingsUpdates(c *ui.Context) {
	settingsHeading(c, i18n.T("更新"), "")
	settingsGroup(c, "updates", func() {
		auto := !a.prefs.NoAutoUpdate
		toggleRow(c, "auto-update", i18n.T("自动更新 FreeMD"), i18n.T("在后台下载新版本，重新启动后安装。关闭后只提示有新版本。"), &auto, nil)
		if auto == a.prefs.NoAutoUpdate {
			a.toggleAutoUpdate()
		}
		settingsSeparator(c)
		settingRow(c, "check", i18n.T("当前版本 ")+Version, a.updateSummary(), func() {
			if ui.Button(c.Key("check-now"), i18n.T("检查更新…")).Clicked() {
				a.checkForUpdates()
			}
		})
	})
}

func (a *App) settingsAbout(c *ui.Context) {
	t := c.Theme()
	ui.Row(c.Key("about-product")).Gap(14).AlignItems(ui.Center).Padding(0, 0, 4, 0).Children(func() {
		ui.Image(c, brandLogo).Size(48, 48).Shrink(0)
		ui.Column(c).Gap(3).Children(func() {
			ui.Text(c, "FreeMD").FontSize(20).FontWeight(600)
			ui.Text(c, i18n.T("版本 ")+Version+" · "+i18n.T("安静地写作，有序地整理。")).FontSize(13).TextColor(t.TextMuted)
		})
	})
	settingsGroup(c, "about-intro", func() {
		settingRow(c, "about-github", i18n.T("开源仓库"), "github.com/OUBIGFA/FreeMD", func() {
			ui.Link(c.Key("repository"), i18n.T("打开"), "https://github.com/OUBIGFA/FreeMD").FontSize(13).
				Label(i18n.T("GitHub 开源仓库")).Tooltip(i18n.T("在浏览器中打开 GitHub 开源仓库"))
		})
		settingsSeparator(c)
		settingRow(c, "about-tour", i18n.T("新手教程"), i18n.T("再看一遍空间、空间颜色、左右滑动、书签、置顶和收藏的介绍。"), func() {
			if ui.Button(c.Key("replay-tour"), i18n.T("重新观看")).Disabled(a.files != nil).Clicked() {
				a.startTour()
			}
		})
	})
}
