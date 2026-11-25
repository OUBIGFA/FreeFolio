package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/fontlibrary"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type fontState struct {
	sample                                     *editor.Editor
	previewing                                 bool
	list                                       ui.ListState
	families                                   []ui.FontFamily
	loaded, loading, open, targetUI            bool
	query, lastQuery, category, preview, hover string
	hoverAt                                    time.Time
	index                                      int
	reveal                                     bool
	installed                                  map[string]bool
	err                                        string
	busy                                       string
	progress                                   *atomic.Int64
	total                                      int64
	cancel                                     context.CancelFunc
}

// backgroundResult runs filesystem work away from the native view and applies
// its result on the window thread. Headless tests run the same work directly.
func (a *App) backgroundResult(work func() func()) {
	if a.win == nil {
		work()()
		return
	}
	win := a.win
	go func() { apply := work(); win.Update(apply) }()
}

func (a *App) bodyFont() string {
	if a.fonts.open && !a.fonts.targetUI && a.fonts.previewing {
		return a.fonts.preview
	}
	return a.prefs.BodyFamily()
}
func (a *App) interfaceFont() string {
	if a.fonts.open && a.fonts.targetUI && a.fonts.previewing {
		return a.fonts.preview
	}
	return a.prefs.InterfaceFont
}
func (a *App) previewFont(name string) { a.fonts.preview = name; a.fonts.previewing = true }
func (a *App) closeFontPicker() {
	a.fonts.open = false
	a.fonts.preview = ""
	a.fonts.previewing = false
}

func (a *App) loadFonts() {
	if a.fonts.loading || a.fonts.loaded {
		return
	}
	a.fonts.loading = true
	dir := filepath.Join(a.cfg.ConfigDir, "fonts")
	a.backgroundResult(func() func() {
		installed := map[string]bool{}
		var problems []string
		for _, f := range fontlibrary.Catalog {
			var data [][]byte
			ready := true
			for _, file := range f.Files {
				b, e := fontlibrary.Read(dir, file)
				if e != nil {
					ready = false
					if !os.IsNotExist(e) {
						problems = append(problems, f.Name+": "+e.Error())
					}
					break
				}
				data = append(data, b)
			}
			if ready {
				for _, b := range data {
					if e := ui.RegisterFont(b, f.Family); e != nil {
						problems = append(problems, f.Name+": "+e.Error())
						ready = false
						break
					}
				}
			}
			installed[f.ID] = ready
		}
		families := ui.FontFamilies()
		return func() {
			a.fonts.families = families
			a.fonts.installed = installed
			a.fonts.loading = false
			a.fonts.loaded = true
			a.fonts.err = strings.Join(problems, "\n")
		}
	})
}

func (a *App) openFontPicker(forUI bool) {
	a.fonts.open = true
	a.fonts.targetUI = forUI
	a.fonts.query = ""
	a.fonts.lastQuery = ""
	a.fonts.category = ""
	a.fonts.preview = ""
	a.fonts.previewing = false
	a.fonts.list = ui.ListState{}
	a.fonts.index = 0
	if a.doc != nil && a.doc.ed != nil {
		a.fonts.sample = editor.FromDocument(a.doc.ed.Committed())
	} else {
		a.fonts.sample = editor.New("# 字体预览\n\n自由的文字胜于一切 · Abc 123\n\n选择字体，预览正文的字形与行距")
	}
	a.fonts.sample.ReadOnly = true
	a.loadFonts()
}
func recentFont(list []string, name string) []string {
	list = slices.DeleteFunc(slices.Clone(list), func(s string) bool { return s == name })
	if name != "" {
		list = append([]string{name}, list...)
	}
	return list[:min(5, len(list))]
}
func (a *App) chooseFont(name string) {
	if a.fonts.targetUI {
		a.prefs.InterfaceFont = name
		a.prefs.RecentInterfaceFonts = recentFont(a.prefs.RecentInterfaceFonts, name)
	} else {
		a.prefs.BodyFont = name
		if name == "" {
			a.prefs.Font = ""
		}
		a.prefs.RecentBodyFonts = recentFont(a.prefs.RecentBodyFonts, name)
	}
	a.closeFontPicker()
	a.savePrefs()
}

func fontGroup(f ui.FontFamily) string {
	n := strings.ToLower(f.Name)
	if f.Monospaced {
		return "等宽"
	}
	for _, s := range []string{"kai", "楷", "hand", "script", "xiaolai", "小赖"} {
		if strings.Contains(n, s) {
			return "楷体与手写"
		}
	}
	for _, s := range []string{"song", "simsun", "宋", "serif", "ming", "times", "georgia", "mincho"} {
		if strings.Contains(n, s) && !strings.Contains(n, "sans") {
			return "宋体与衬线"
		}
	}
	for _, s := range []string{"round", "圆", "maru"} {
		if strings.Contains(n, s) {
			return "圆体"
		}
	}
	for _, s := range []string{"sans", "hei", "黑", "arial", "segoe", "gothic", "苹方", "微软雅黑"} {
		if strings.Contains(n, s) {
			return "黑体与无衬线"
		}
	}
	return "其他字体"
}

type fontChoice struct {
	name, label, group string
	download           *fontlibrary.Font
}

func (a *App) fontChoices() []fontChoice {
	f := &a.fonts
	var rows []fontChoice
	match := func(s string) bool {
		return strings.Contains(strings.ToLower(s), strings.ToLower(strings.TrimSpace(f.query)))
	}
	rows = append(rows, fontChoice{label: i18n.T("跟随系统字体")})
	byName := map[string]ui.FontFamily{}
	catalogNames := map[string]bool{}
	for _, entry := range fontlibrary.Catalog {
		catalogNames[entry.Family] = true
	}
	for _, family := range f.families {
		if !catalogNames[family.Name] {
			byName[family.Name] = family
		}
	}
	appendNamed := func(names []string, group string) {
		for _, name := range names {
			if catalogNames[name] {
				for i := range fontlibrary.Catalog {
					entry := &fontlibrary.Catalog[i]
					if entry.Family == name && match(entry.Name+name) {
						rows = append(rows, fontChoice{name: name, label: entry.Name, group: group, download: entry})
					}
				}
				continue
			}
			family, ok := byName[name]
			if ok && match(name) {
				rows = append(rows, fontChoice{name: family.Name, label: family.Name, group: group})
			}
		}
	}
	appendNamed(a.prefs.FavoriteFonts, "收藏字体")
	recent := a.prefs.RecentBodyFonts
	if f.targetUI {
		recent = a.prefs.RecentInterfaceFonts
	}
	appendNamed(recent, "最近使用")
	for _, group := range []string{"黑体与无衬线", "宋体与衬线", "楷体与手写", "圆体", "等宽", "其他字体"} {
		for _, family := range f.families {
			if _, ok := byName[family.Name]; ok && fontGroup(family) == group && match(family.Name) {
				rows = append(rows, fontChoice{name: family.Name, label: family.Name, group: group})
			}
		}
	}
	for i := range fontlibrary.Catalog {
		entry := &fontlibrary.Catalog[i]
		if match(entry.Name + entry.Family) {
			rows = append(rows, fontChoice{name: entry.Family, label: entry.Name, group: "可下载字体", download: entry})
		}
	}
	if f.category != "" {
		filtered := rows[:0]
		for _, row := range rows {
			if row.group == f.category {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	return rows
}

func (a *App) downloadFont(f fontlibrary.Font) {
	if a.fonts.busy != "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.fonts.cancel = cancel
	a.fonts.busy = f.ID
	a.fonts.err = ""
	a.fonts.progress = &atomic.Int64{}
	a.fonts.total = 0
	for _, file := range f.Files {
		a.fonts.total += file.Bytes
	}
	progress := a.fonts.progress
	dir := filepath.Join(a.cfg.ConfigDir, "fonts")
	targetUI := a.fonts.targetUI
	a.backgroundResult(func() func() {
		defer cancel()
		err := fontlibrary.Download(ctx, nil, dir, f.Files, func(n, total int64) { progress.Store(n) })
		if err == nil {
			for _, file := range f.Files {
				b, e := fontlibrary.Read(dir, file)
				if e == nil {
					e = ui.RegisterFont(b, f.Family)
				}
				if e != nil {
					err = e
					break
				}
			}
		}
		return func() {
			a.fonts.busy = ""
			a.fonts.cancel = nil
			if err != nil {
				a.fonts.err = err.Error()
				return
			}
			if a.fonts.installed == nil {
				a.fonts.installed = map[string]bool{}
			}
			a.fonts.installed[f.ID] = true
			a.fonts.families = ui.FontFamilies()
			if a.fonts.open && a.fonts.targetUI == targetUI {
				a.chooseFont(f.Family)
			}
		}
	})
}

func (a *App) removeFont(f fontlibrary.Font) {
	if a.fonts.busy != "" {
		return
	}
	a.fonts.busy = f.ID
	dir := filepath.Join(a.cfg.ConfigDir, "fonts")
	trash := a.trashItem
	a.backgroundResult(func() func() {
		var err error
		for _, file := range f.Files {
			p := filepath.Join(dir, file.Name)
			if _, e := os.Lstat(p); os.IsNotExist(e) {
				continue
			} else if e != nil {
				err = e
				break
			}
			if err = trash(p); err != nil {
				break
			}
		}
		return func() {
			a.fonts.busy = ""
			if err != nil {
				a.fonts.err = err.Error()
				return
			}
			a.fonts.installed[f.ID] = false
			if a.prefs.BodyFont == f.Family {
				a.prefs.BodyFont = ""
			}
			if a.prefs.InterfaceFont == f.Family {
				a.prefs.InterfaceFont = ""
			}
			a.fonts.preview = ""
			a.savePrefs()
		}
	})
}

func (a *App) fontPickerView(c *ui.Context) {
	f := &a.fonts
	if !f.open {
		return
	}
	t := c.Theme()
	w, h := c.Size()
	chromeFont := a.prefs.InterfaceFont
	if chromeFont == "" {
		chromeFont = "system-ui"
	}
	previewUIFont := a.interfaceFont()
	if previewUIFont == "" {
		previewUIFont = "system-ui"
	}
	activeFont := a.prefs.BodyFamily()
	if f.targetUI {
		activeFont = a.prefs.InterfaceFont
	}
	ui.Modal(c.Key("font-picker"), &f.open, func() {
		rows := a.fontChoices()
		if f.query != f.lastQuery {
			f.index = 0
			f.lastQuery = f.query
			f.reveal = true
		}
		moved := false
		if c.Shortcut(0, ui.KeyDown) {
			f.index++
			moved = true
		}
		if c.Shortcut(0, ui.KeyUp) {
			f.index--
			moved = true
		}
		if len(rows) > 0 {
			f.index = max(0, min(f.index, len(rows)-1))
		} else {
			f.index = 0
		}
		preview := func(row fontChoice) {
			if row.download == nil || f.installed[row.download.ID] {
				a.previewFont(row.name)
			}
		}
		choose := func(row fontChoice) {
			if row.download != nil && !f.installed[row.download.ID] {
				a.downloadFont(*row.download)
			} else {
				a.chooseFont(row.name)
			}
		}
		if moved && len(rows) > 0 {
			f.reveal = true
			preview(rows[f.index])
		}
		modalW := min(800, w-64)
		modalH := min(580, h-64)
		ui.Column(c.Key("font-picker-dialog")).Font(chromeFont).Width(modalW).Height(modalH).Gap(10).Children(func() {
			title := i18n.T("正文字体")
			if f.targetUI {
				title = i18n.T("界面字体")
			}
			activeLabel := activeFont
			if activeLabel == "" {
				activeLabel = i18n.T("跟随系统字体")
			}
			ui.Row(c.Key("font-picker-header")).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(17).FontWeight(600)
				ui.Row(c).Padding(2, 8).Radius(5).Background(t.Surface).Border(1, t.Border.Alpha(0.75)).AlignItems(ui.Center).Shrink(0).Children(func() {
					ui.Text(c, i18n.T("当前：")+activeLabel).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
				})
				ui.Spacer(c)
				if activeFont != "" {
					if ui.Button(c.Key("font-reset"), i18n.T("恢复默认")).Clicked() {
						a.chooseFont("")
						return
					}
				}
				if ui.Button(c.Key("font-close"), i18n.T("取消预览")).Clicked() {
					a.closeFontPicker()
				}
			})
			ui.Column(c.Key("font-picker-filters")).Shrink(0).Gap(8).Children(func() {
				input := ui.TextInput(c.Key("font-query"), &f.query).Placeholder(i18n.T("搜索字体")).Label(i18n.T("搜索字体")).AutoFocus()
				if input.Submitted() && len(rows) > 0 {
					choose(rows[f.index])
					return
				}
				cats := []struct{ id, label string }{
					{"", "全部"},
					{"收藏字体", "收藏"},
					{"黑体与无衬线", "黑体"},
					{"宋体与衬线", "宋体"},
					{"楷体与手写", "楷体/手写"},
					{"等宽", "等宽"},
					{"可下载字体", "可下载"},
				}
				ui.Row(c.Key("font-categories")).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					for _, cat := range cats {
						active := f.category == cat.id
						chip := ui.ButtonBase(c.Key("font-cat-" + cat.id)).Height(26).PaddingX(10).Radius(6).Center().Focusable().
							Cursor(ui.CursorPointer).Label(i18n.T(cat.label))
						if active {
							chip.Background(t.Accent.Alpha(0.14)).Border(1, t.Accent.Alpha(0.45))
						} else if chip.Hovered() {
							chip.Background(hoverFill(t)).Border(1, t.Border.Alpha(0.7))
						} else {
							chip.Background(t.Surface).Border(1, t.Border.Alpha(0.7))
						}
						ink := t.TextMuted
						if active {
							ink = t.Accent
						}
						chip.Children(func() {
							ui.Text(c, i18n.T(cat.label)).FontSize(11.5).FontWeight(500).TextColor(ink).SingleLine()
						})
						if chip.Clicked() && f.category != cat.id {
							f.category = cat.id
							f.index = 0
							f.reveal = true
						}
					}
				})
			})
			if f.loading {
				ui.Row(c.Key("font-loading")).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c.Key("font-load-spinner")).TextColor(t.Accent).Label(i18n.T("正在读取字体"))
					ui.Text(c, i18n.T("正在读取字体")).FontSize(12.5).TextColor(t.TextMuted)
				})
			}
			if f.err != "" {
				ui.Row(c.Key("font-err")).Shrink(0).Padding(8, 12).Gap(8).AlignItems(ui.Center).
					Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.35)).Radius(8).Children(func() {
					ui.Icon(c, iconAlert).FontSize(14).TextColor(t.Danger).Shrink(0)
					ui.Text(c, f.err).TextColor(t.Danger).FontSize(12.5).Grow(1).MinWidth(0).LineHeight(1.4)
				})
			}
			if f.busy != "" {
				pct := int64(0)
				progressVal := float64(-1)
				text := i18n.T("正在处理字体")
				if f.progress != nil && f.total > 0 {
					pct = min(100, max(0, f.progress.Load()*100/f.total))
					progressVal = float64(f.progress.Load()) / float64(f.total)
					text = fmt.Sprintf(i18n.T("正在下载字体：%d%%"), pct)
				}
				ui.Column(c.Key("font-busy")).Shrink(0).Padding(8, 12).Gap(6).Background(t.Surface).Border(1, t.Border).Radius(8).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, text).FontSize(12.5).Grow(1)
						if f.cancel != nil && ui.Button(c.Key("font-cancel-download"), i18n.T("取消下载")).Clicked() {
							f.cancel()
						}
					})
					ui.Progress(c.Key("font-download-progress"), progressVal).Label(text)
				})
				c.After(100 * time.Millisecond)
			}

			renderList := func() {
				if len(rows) == 0 {
					ui.Column(c.Key("font-empty")).Grow(1).MinHeight(0).Center().Gap(6).Children(func() {
						ui.Text(c, i18n.T("没有匹配的字体")).FontSize(13).FontWeight(500)
						ui.Text(c, i18n.T("尝试调整搜索关键词或切换分类")).FontSize(12).TextColor(t.TextMuted)
					})
					return
				}
				if f.reveal {
					f.list.ScrollIntoView(f.index)
				}
				ui.List(c.Key("font-list"), &f.list, len(rows)).Grow(1).MinHeight(0).ItemKey(func(i int) any { return rows[i].group + "\x00" + rows[i].name }).Rows(func(item ui.ListRow) {
					i, row, c := item.Index, rows[item.Index], item.Context
					if row.group != "" && (i == 0 || row.group != rows[i-1].group) {
						ui.Text(c, i18n.T(row.group)).FontSize(11.5).FontWeight(600).TextColor(t.TextMuted).Padding(8, 4, 4, 4)
					}
					line := ui.Row(c.Key(fmt.Sprintf("font-%s-%s", row.group, row.name))).Padding(6, 8).Gap(8).Radius(6).AlignItems(ui.Center)
					if i == f.index {
						line.Background(t.Accent.Alpha(.12))
						if f.reveal {
							line.ScrollIntoView()
						}
					}
					line.Children(func() {
						specimenFont := row.name
						if specimenFont == "" {
							specimenFont = "system-ui"
						}
						b := ui.ButtonBase(c.Key("pick")).Label(row.label).Grow(1).MinWidth(0).Justify(ui.Start).Cursor(ui.CursorPointer)
						b.Children(func() {
							ui.Column(c).Gap(2).Children(func() {
								ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
									ui.Text(c, row.label).FontSize(12.5).FontWeight(500).SingleLine()
									if row.name == activeFont {
										ui.Icon(c, iconCheck).FontSize(12).TextColor(t.Accent).Shrink(0)
									}
								})
								ui.Text(c, i18n.T("自由的文字胜于一切 · Abc 123")).Font(specimenFont).FontSize(16).SingleLine()
							})
						})
						if b.Hovered() {
							if f.hover != row.name {
								f.hover = row.name
								f.hoverAt = c.Now()
							}
							if c.Now().Sub(f.hoverAt) >= 160*time.Millisecond {
								f.index = i
								preview(row)
							} else {
								c.After(160 * time.Millisecond)
							}
						}
						if b.Clicked() {
							choose(row)
						}
						if row.name != "" {
							starred := slices.Contains(a.prefs.FavoriteFonts, row.name)
							label := i18n.T("收藏字体")
							if starred {
								label = i18n.T("取消收藏字体")
							}
							if iconButton(c, "font-star", iconStar, label, label, starred).Clicked() {
								if starred {
									a.prefs.FavoriteFonts = slices.DeleteFunc(a.prefs.FavoriteFonts, func(s string) bool { return s == row.name })
								} else {
									a.prefs.FavoriteFonts = append(a.prefs.FavoriteFonts, row.name)
								}
								a.savePrefs()
							}
						}
						if row.download != nil {
							if f.installed[row.download.ID] {
								if ui.Button(c.Key("remove"), i18n.T("移除字体")).Disabled(f.busy != "").Clicked() {
									a.removeFont(*row.download)
								}
							} else {
								var size int64
								for _, file := range row.download.Files {
									size += file.Bytes
								}
								ui.Text(c, fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))).FontSize(11.5).TextColor(t.TextMuted).Shrink(0)
							}
						}
					})
				})
				f.reveal = false
			}

			renderPreview := func() {
				previewTitle := activeLabel
				previewGroup := ""
				if len(rows) > 0 && f.index < len(rows) {
					if f.previewing {
						previewTitle = rows[f.index].label
						previewGroup = rows[f.index].group
					} else {
						for _, r := range rows {
							if r.name == activeFont {
								previewTitle = r.label
								previewGroup = r.group
								break
							}
						}
					}
				}
				ui.Row(c.Key("font-preview-head")).Height(26).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, previewTitle).FontSize(13).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
					if previewGroup != "" {
						ui.Row(c).Padding(2, 6).Radius(4).Background(t.Surface).Border(1, t.Border.Alpha(0.7)).Shrink(0).Children(func() {
							ui.Text(c, i18n.T(previewGroup)).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
						})
					}
				})
				if f.targetUI {
					// Fixed-height slots and fixed-width buttons keep every preview control
					// at stable coordinates regardless of the previewed font's line metrics.
					ui.Column(c.Key("font-ui-preview")).Grow(1).MinHeight(0).Padding(14).Background(t.Background).
						Border(1, t.Border).Radius(8).Font(previewUIFont).Clip().Children(func() {
						ui.Row(c).Height(28).Shrink(0).AlignItems(ui.Center).Children(func() {
							ui.Text(c, i18n.T("界面字体")).FontSize(16).FontWeight(600).SingleLine()
						})
						ui.Row(c).Height(26).Shrink(0).AlignItems(ui.Center).Children(func() {
							ui.Text(c, i18n.T("自由的文字胜于一切 · Abc 123")).FontSize(14).SingleLine()
						})
						ui.Row(c).Height(22).Shrink(0).AlignItems(ui.Center).Children(func() {
							ui.Text(c, i18n.T("0123456789 · 菜单与按钮排版")).FontSize(12).TextColor(t.TextMuted).SingleLine()
						})
						ui.Box(c).Height(1).Shrink(0).Margin(8, 0).Background(t.Border.Alpha(0.6))
						ui.Row(c).Height(30).Shrink(0).PaddingX(8).Radius(6).Background(t.Surface).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, iconFile).FontSize(12).TextColor(t.TextMuted).Shrink(0)
							ui.Text(c, i18n.T("文档列表")).FontSize(12.5).SingleLine().Grow(1).MinWidth(0)
							ui.Text(c, "⌘K").FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(0)
						})
						ui.Spacer(c)
						ui.Row(c.Key("font-ui-preview-actions")).Height(36).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Button(c.Key("preview-btn-settings"), i18n.T("设置")).Width(80).Height(32)
							ui.PrimaryButton(c.Key("preview-btn-save"), i18n.T("保存")).Width(80).Height(32)
						})
					})
				} else if f.sample != nil {
					paper := documentTheme(a.paperDark)
					previewW := float32(320)
					if modalW < 620 {
						previewW = modalW
					}
					ui.Themed(c.Key("font-page-preview"), paper).Grow(1).MinHeight(0).Background(paper.Background).
						Border(1, paper.Border).Radius(8).Clip().Children(func() {
						editor.View(c.Key("font-sample"), f.sample, editor.Options{Width: previewW, Theme: paper, Page: pageStyle(a.paperDark), FontFamily: a.bodyFont(), FontSize: a.prefs.ReadingSize(), LineHeight: a.prefs.ReadingLine(), Column: -1})
					})
				}
				ui.Row(c.Key("font-preview-footer")).Height(34).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T("↑↓ 预览 · Enter 应用")).FontSize(11.5).TextColor(t.TextMuted).Grow(1).SingleLine()
					if ui.PrimaryButton(c.Key("font-apply"), i18n.T("应用字体")).Disabled(len(rows) == 0).Clicked() && len(rows) > 0 {
						choose(rows[f.index])
					}
				})
			}

			ui.Box(c).Height(1).Shrink(0).Background(t.Border.Alpha(0.6))
			if modalW >= 620 {
				ui.Row(c.Key("font-picker-body")).Grow(1).MinHeight(0).Gap(12).AlignItems(ui.Stretch).Children(func() {
					ui.Column(c.Key("font-list-pane")).Grow(1).MinWidth(0).MinHeight(0).Children(renderList)
					ui.Box(c).Width(1).Shrink(0).Background(t.Border.Alpha(0.6))
					ui.Column(c.Key("font-preview-pane")).Width(320).Shrink(0).MinHeight(0).Gap(8).Children(renderPreview)
				})
			} else {
				ui.Column(c.Key("font-picker-body")).Grow(1).MinHeight(0).Gap(10).Children(func() {
					ui.Column(c.Key("font-list-pane")).Grow(1).MinHeight(0).Children(renderList)
					ui.Box(c).Height(1).Shrink(0).Background(t.Border.Alpha(0.6))
					ui.Column(c.Key("font-preview-pane")).Height(220).Shrink(0).Gap(6).Children(renderPreview)
				})
			}
		})
	})
}
