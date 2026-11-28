package app

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/database"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Version history (package history): saving a document takes a version of
// it, at most one per ten minutes of editing, and the first save of a
// session keeps the version the document had before. The history window
// lists them, shows what each differs from the document now, and brings one
// back as one step of the editor's history.

// versionsHost is all the history window asks of the window around it: the
// open document, saving, background work, notices and the shared chrome.
// The window works through this narrow interface rather than App, so it can
// change without touching App and is a template for splitting App further.
type versionsHost interface {
	activeDocument() *document
	autosaveEnabled() bool
	flush() bool
	backgroundResult(work func() func())
	boardChanged(d *document)
	notify(format string, args ...any)
	imageResourceFor(d *document, url string) editor.ImageResource
	retireClosedDocuments(candidates ...*document)
	titleBar(c *ui.Context) ui.TitleBar
	paperIsDark() bool
	preferences() *prefs.Prefs
	window() *mygo.Window
}

// versionsPanel is the history window: its state and behavior.
type versionsPanel struct {
	host versionsHost

	dbText string

	loading   bool
	preview   *editor.Editor
	marks     map[string]bool
	showDiff  bool
	showMarks bool
	board     *editor.Board
	open      bool
	err       error
	versions  []history.Version
	sel       int
	// for is the document the versions are of; shown the version whose
	// difference diff holds.
	for_           *document
	shown          string
	doc            *model.Document
	diff           []history.Line
	added          int
	gone           int
	milestonesOnly bool
	creating       bool
	createLabel    string
	createFocused  bool
	renamingFile   string
	renameLabel    string
	renameFocused  bool
}

// App hosts the history window; these accessors complete versionsHost.
func (a *App) activeDocument() *document { return a.doc }
func (a *App) paperIsDark() bool         { return a.paperDark }
func (a *App) preferences() *prefs.Prefs { return a.prefs }
func (a *App) window() *mygo.Window      { return a.win }

// recordVersion takes a version of the document as just written; before
// its first write this session, the version it had on disk is kept.
func (d *document) recordVersion(before string) {
	if d.historyOff {
		return
	}
	root := d.space.Root
	if !d.historyStarted && before != "" {
		d.historyStarted = true
		at := time.Now().Add(-time.Second)
		if info, err := os.Stat(d.fullPath()); err == nil && info.ModTime().Before(at) {
			at = info.ModTime()
		}
		_ = history.Record(root, d.rel, before, at, true)
	}
	d.historyStarted = true
	_ = history.Record(root, d.rel, d.diskText, time.Now(), false)
}

// fullPath is the document's file.
func (d *document) fullPath() string {
	p, _ := resolveFile(d.space, d.rel)
	return p
}

// shownPath is fullPath for display, as the title bar's tooltip, which
// every frame asks for (keptPath).
func (d *document) shownPath() string {
	p, _ := d.shown.resolve(d.space, d.rel)
	return p
}

// show opens the history of the open document.
func (v *versionsPanel) show() {
	d := v.host.activeDocument()
	if d == nil || d.loadErr != nil || d.isPDF() || d.isMDZ() {
		v.host.notify("这篇文档还没有版本历史")
		return
	}
	if v.host.autosaveEnabled() {
		v.host.flush()
	}
	*v = versionsPanel{host: v.host, open: true, for_: d, showMarks: true}
	v.loading = true
	root, rel := d.space.Root, d.rel
	v.host.backgroundResult(func() func() {
		versions, err := history.List(root, rel)
		return func() {
			if !v.open || v.for_ != d {
				return
			}
			v.loading = false
			v.versions, v.err = versions, err
			if err == nil && len(versions) > 0 {
				v.selectVersion(0)
			}
		}
	})
}

// selectVersion reads the version at i and compares it with the document
// as it is.
func (v *versionsPanel) selectVersion(i int) {
	if i < 0 || i >= len(v.versions) || v.for_ == nil {
		return
	}
	v.sel = i
	ver := v.versions[i]
	if v.shown == ver.File {
		return
	}
	v.shown, v.doc, v.diff, v.err = ver.File, nil, nil, nil
	v.board = nil
	v.dbText = ""
	v.preview = nil
	v.loading = true
	d := v.for_
	root, rel := d.space.Root, d.rel
	base := d.ed.Committed().Clone()
	isBoard := d.board != nil
	isDB := d.isDatabase()
	current := base.PlainText()
	if isDB {
		current = extractDatabaseSearchText(d.rel, d.text()).Text
	}
	if isBoard {
		current = d.board.Scene.PlainText()
	}
	v.host.backgroundResult(func() func() {
		text, err := history.Read(root, rel, ver)
		var doc *model.Document
		var scene *whiteboard.Scene
		var diff []history.Line
		var marks map[string]bool
		if err == nil {
			if isDB {
				var db *database.Database
				db, err = database.DecodeDatabaseMarkdown(text)
				if err == nil {
					doc = editor.ImportMarkdown(database.RenderGFMSnapshot(parentDir(rel), db, nil))
					diff = history.Diff(strings.Split(current, "\n"), strings.Split(extractDatabaseSearchText(rel, text).Text, "\n"))
				}
			} else if isBoard {
				scene, err = whiteboard.Decode(text)
				if err == nil {
					doc = model.New()
					diff = history.Diff(strings.Split(current, "\n"), strings.Split(scene.PlainText(), "\n"))
				}
			} else {
				if strings.EqualFold(path.Ext(ver.File), ".arcmd") {
					doc, err = model.Decode(text)
				} else {
					doc = editor.ImportMarkdown(text)
				}
				if err == nil {
					marks = model.ChangedBlocks(base, doc)
					diff = history.Diff(strings.Split(current, "\n"), strings.Split(doc.PlainText(), "\n"))
				}
			}
		}
		return func() {
			if !v.open || v.for_ != d || v.shown != ver.File {
				return
			}
			v.loading = false
			v.err = err
			if err != nil {
				return
			}
			v.doc, v.diff, v.marks = doc, diff, marks
			if isDB {
				v.dbText = text
			}
			if scene != nil {
				v.board = editor.NewBoard(scene)
			} else {
				v.preview = editor.FromDocument(doc)
				v.preview.ReadOnly = true
			}
			v.added, v.gone = 0, 0
			for _, line := range diff {
				if line.Op == history.Added {
					v.added++
				} else if line.Op == history.Removed {
					v.gone++
				}
			}
		}
	})
}

// restoreVersion puts the version shown in place of the document, as one
// step of the editor's history.
func (v *versionsPanel) restoreVersion() {
	doc := v.host.activeDocument()
	if v.doc == nil || v.for_ != doc {
		return
	}
	if v.board != nil && doc.board != nil {
		doc.board.ReplaceScene(v.board.Scene)
		v.host.boardChanged(doc)
		v.open = false
		return
	}
	at := v.versions[v.sel].Time
	if v.dbText != "" && doc.isDatabase() {
		db, err := database.DecodeDatabaseMarkdown(v.dbText)
		if err == nil {
			err = doc.dbView.Store.RestoreSchema(db)
		}
		if err != nil {
			v.host.notify("恢复数据库版本失败；%v", err)
			return
		}
		doc.dbDirtySnapshot = true
		v.open = false
		v.host.notify("已恢复到 %s 的版本，可用 ⌘Z 撤销", versionTime(at, time.Now()))
		return
	}
	doc.ed.Engine.ReplaceDocument(v.doc)
	if doc.source {
		doc.refreshSourceFromModel()
		doc.sourceFocusPending = true
	} else {
		doc.ed.RequestFocus = true
	}
	v.open = false
	v.host.notify("已恢复到 %s 的版本，可用 ⌘Z 撤销", versionTime(at, time.Now()))
}

func (v *versionsPanel) createMilestone() {
	d := v.for_
	label := strings.TrimSpace(v.createLabel)
	if d == nil || label == "" {
		return
	}
	if d.dirty() {
		v.host.flush()
	}
	content := d.diskText
	if content == "" {
		committed := d.ed.Committed()
		if strings.EqualFold(path.Ext(d.rel), ".arcmd") {
			content, _ = committed.Encode()
		} else {
			content = editor.ExportMarkdown(committed)
		}
	}
	saved, err := history.SaveMilestone(d.space.Root, d.rel, content, label, time.Now())
	if err != nil {
		v.err = err
		return
	}
	v.creating, v.createLabel, v.createFocused = false, "", false
	v.versions, v.err = history.List(d.space.Root, d.rel)
	v.shown = ""
	for i, ver := range v.versions {
		if ver.File == saved.File {
			v.selectVersion(i)
			break
		}
	}
	v.host.notify("已保存里程碑「%s」", label)
}

func (v *versionsPanel) renameMilestone(file string) {
	if v.for_ == nil || file == "" {
		return
	}
	label := strings.TrimSpace(v.renameLabel)
	v.renamingFile, v.renameLabel, v.renameFocused = "", "", false
	if err := history.SetLabel(v.for_.space.Root, v.for_.rel, file, label); err != nil {
		v.err = err
		return
	}
	v.versions, v.err = history.List(v.for_.space.Root, v.for_.rel)
}

func (v *versionsPanel) toggleMilestonesFilter(only bool) {
	v.milestonesOnly = only
	if !only || len(v.versions) == 0 {
		return
	}
	if v.sel >= 0 && v.sel < len(v.versions) && v.versions[v.sel].Label != "" {
		return
	}
	for i, ver := range v.versions {
		if ver.Label != "" {
			v.selectVersion(i)
			return
		}
	}
}

// versionTime says when a version was taken, as people say it.
func versionTime(at, now time.Time) string {
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return i18n.T("今天 ") + at.Format("15:04")
	case y1 == y2 && m1 == m2 && d1 == d2-1:
		return i18n.T("昨天 ") + at.Format("15:04")
	case y1 == y2:
		return fmt.Sprintf(i18n.T("%d月%d日 %s"), m1, d1, at.Format("15:04"))
	}
	return fmt.Sprintf(i18n.T("%d年%d月%d日 %s"), y1, m1, d1, at.Format("15:04"))
}

// view draws the history window.
func (v *versionsPanel) view(c *ui.Context) {
	if v.open && v.for_ != v.host.activeDocument() {
		v.open = false
	}
	if !v.open {
		old := v.for_
		*v = versionsPanel{host: v.host}
		v.host.retireClosedDocuments(old)
		return
	}
	t := documentTheme(v.host.paperIsDark())
	c.SetTheme(t)
	w, h := c.Size()
	bar := v.host.titleBar(c)
	if c.Shortcut(0, ui.KeyEscape) {
		v.open = false
		return
	}
	ui.Column(c.Key("versions")).Fill().Background(t.Background).Children(func() {
		ui.Column(c.Key("versions-body")).Size(w, h).Padding(12).Gap(12).Children(func() {
			ui.Row(c).Height(max(36, bar.Height)).Padding(0, bar.Right, 0, bar.Left).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
				ui.Icon(c, iconHistory).FontSize(16).TextColor(t.TextMuted)
				if ui.Button(c.Key("history-return"), i18n.T("返回当前文档")).Clicked() {
					v.open = false
				}
				ui.Text(c, i18n.T("版本历史 · ")+v.for_.title()).FontSize(16).FontWeight(600).Grow(1).SingleLine().MinWidth(0)
				if ui.Button(c.Key("version-milestone-new"), i18n.T("保存里程碑")).Clicked() {
					v.creating = !v.creating
					v.createLabel = ""
					v.createFocused = false
				}
				if v.doc != nil {
					if ui.PrimaryButton(c.Key("version-restore"), i18n.T("恢复此版本")).Clicked() {
						v.restoreVersion()
					}
				}
			})
			if v.creating {
				ui.Row(c.Key("version-milestone-bar")).AlignItems(ui.Center).Gap(8).Children(func() {
					in := ui.TextInput(c.Key("version-milestone-input"), &v.createLabel).
						Placeholder(i18n.T("里程碑名称（如：定稿 v1）")).
						Label(i18n.T("里程碑名称（如：定稿 v1）")).
						Background(field(t)).Grow(1).MinWidth(0).Padding(6, 10)
					if !v.createFocused {
						in.Focus()
					}
					if in.Focused() {
						v.createFocused = true
					}
					if in.Submitted() {
						v.createMilestone()
					}
					saveBtn := ui.PrimaryButton(c.Key("version-milestone-save"), i18n.T("保存")).
						Disabled(strings.TrimSpace(v.createLabel) == "")
					if saveBtn.Clicked() {
						v.createMilestone()
					}
					if ui.Button(c.Key("version-milestone-cancel"), i18n.T("取消")).Clicked() {
						v.creating, v.createLabel, v.createFocused = false, "", false
					}
				})
			}
			if len(v.versions) == 0 {
				ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
					switch {
					case v.loading:
						ui.Text(c, i18n.T("正在读取历史版本"))
					case v.err != nil:
						ui.Text(c, i18n.T("读取版本历史失败")+" "+v.err.Error()).TextColor(t.Danger)
					default:
						ui.Text(c, i18n.T("还没有历史版本")).FontSize(15)
						ui.Text(c, i18n.T("编辑并保存后，FreeFolio 会自动保留版本（每 10 分钟至多一个，最多 100 个），保存在空间文件夹的 .freefolio/history 里")).
							FontSize(12).TextColor(t.TextMuted).MaxWidth(420).TextAlign(ui.Center)
					}
				})
				return
			}
			ui.Row(c).Grow(1).MinHeight(0).Gap(12).AlignItems(ui.Stretch).Reverse().Children(func() {
				ui.Column(c).Width(210).MinHeight(0).Gap(8).Children(func() {
					ui.Row(c.Key("version-filter")).Gap(4).Padding(2).Radius(8).Background(t.Text.Alpha(0.05)).Children(func() {
						filterBtn := func(key, label string, active bool, onClick func()) {
							b := ui.ButtonBase(c.Key(key)).Grow(1).Radius(6).Padding(4, 8).Cursor(ui.CursorPointer)
							if active {
								b.Background(t.Background)
							} else if b.Hovered() {
								b.Background(hoverFill(t))
							}
							col := t.TextMuted
							if active {
								col = t.Text
							}
							b.Children(func() {
								ui.Text(c, i18n.T(label)).FontSize(12).FontWeight(500).TextColor(col)
							})
							if b.Clicked() {
								onClick()
							}
						}
						filterBtn("filter-all", "全部", !v.milestonesOnly, func() { v.toggleMilestonesFilter(false) })
						filterBtn("filter-milestones", "里程碑", v.milestonesOnly, func() { v.toggleMilestonesFilter(true) })
					})
					ui.Scroll(c.Key("version-list")).Grow(1).MinHeight(0).Children(func() {
						now := time.Now()
						shownCount := 0
						for i, ver := range v.versions {
							if v.milestonesOnly && ver.Label == "" {
								continue
							}
							shownCount++
							if v.renamingFile == ver.File {
								ui.Row(c.Key("version-rename-row:"+ver.File)).Padding(4, 6).Children(func() {
									in := ui.TextInput(c.Key("version-rename:"+ver.File), &v.renameLabel).
										Placeholder(i18n.T("里程碑名称（如：定稿 v1）")).
										Label(i18n.T("命名里程碑")).
										Background(field(t)).Grow(1).MinWidth(0).Padding(4, 8)
									if !v.renameFocused {
										in.Focus()
									}
									left := v.renameFocused && !in.Focused()
									if in.Focused() {
										v.renameFocused = true
									}
									if in.Submitted() || left {
										v.renameMilestone(ver.File)
									}
								})
								continue
							}
							on := i == v.sel
							rowWrap := ui.Row(c.Key("version-row:"+ver.File)).AlignItems(ui.Center).Radius(8).Gap(4).Padding(2, 4)
							switch {
							case on:
								rowWrap.Background(t.Accent.Alpha(0.14))
							case rowWrap.Hovered():
								rowWrap.Background(hoverFill(t))
							}
							rowWrap.ContextMenu(func(m *ui.Menu) {
								if m.Item(i18n.T("命名里程碑")).Chosen() {
									v.renamingFile, v.renameLabel, v.renameFocused = ver.File, ver.Label, false
								}
							})
							rowWrap.Children(func() {
								row := ui.ButtonBase(c.Key("version:"+ver.File)).Grow(1).MinWidth(0).Padding(6, 6).Radius(6).Gap(2).
									Label(i18n.T("版本 ") + versionTime(ver.Time, now)).Cursor(ui.CursorPointer).Justify(ui.Start)
								row.Children(func() {
									ui.Column(c).Grow(1).MinWidth(0).Gap(3).AlignItems(ui.Start).Children(func() {
										if ver.Label != "" {
											ui.Row(c.Key("version-badge:"+ver.File)).AlignItems(ui.Center).Gap(4).Padding(2, 6).Radius(5).Background(t.Accent.Alpha(0.12)).Children(func() {
												ui.Icon(c, iconStar).FontSize(11).TextColor(t.Accent)
												ui.Text(c, ver.Label).FontSize(11).FontWeight(600).TextColor(t.Accent).SingleLine().MinWidth(0)
											})
										}
										ui.Text(c, versionTime(ver.Time, now)).FontSize(13).FontWeight(500)
										note := strings.TrimSpace(ago(ver.Time, now))
										if ver.Kept && ver.Label == "" {
											note += i18n.T(" · 打开时的版本")
										}
										ui.Text(c, note).FontSize(11).TextColor(t.TextMuted)
									})
								})
								if row.Clicked() {
									v.selectVersion(i)
								}
								if on || rowWrap.Hovered() {
									editBtn := ui.ButtonBase(c.Key("version-rename-btn:" + ver.File)).Padding(4).Radius(6).Cursor(ui.CursorPointer).Tooltip(i18n.T("命名里程碑"))
									if on {
										editBtn.Label(i18n.T("命名里程碑"))
									}
									if editBtn.Hovered() {
										editBtn.Background(hoverFill(t))
									}
									editBtn.Children(func() {
										ui.Icon(c, iconPencil).FontSize(12).TextColor(t.TextMuted)
									})
									if editBtn.Clicked() {
										v.renamingFile, v.renameLabel, v.renameFocused = ver.File, ver.Label, false
									}
								}
							})
						}
						if v.milestonesOnly && shownCount == 0 {
							ui.Column(c.Key("version-milestones-empty")).Padding(24, 8).Center().Children(func() {
								ui.Text(c, i18n.T("还没有命名的里程碑")).FontSize(12).TextColor(t.TextMuted).TextAlign(ui.Center)
							})
						}
					})
				})
				ui.Box(c).Width(1).Background(t.Text.Alpha(0.08))
				ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
					if v.loading {
						ui.Text(c, i18n.T("正在读取历史版本"))
						return
					}
					ui.Row(c.Key("history-display")).Gap(8).AlignItems(ui.Center).Children(func() {
						mode := 0
						if v.showDiff {
							mode = 1
						}
						segmentedChoice(c.Key("history-mode"), &mode, i18n.T("正文预览"), i18n.T("文字差异"))
						v.showDiff = mode == 1
						if !v.showDiff {
							ui.Checkbox(c.Key("history-marks"), &v.showMarks, i18n.T("显示改动"))
						}
					})
					if v.board != nil {
						editor.BoardView(c, v.board, editor.BoardOptions{ReadOnly: true, Theme: c.Theme()})
						return
					}
					if !v.showDiff && v.preview != nil {
						if v.added > 0 || v.gone > 0 {
							ui.Text(c, fmt.Sprintf(i18n.T("与当前内容相比：恢复后增加 %d 行、删除 %d 行"), v.added, v.gone)).FontSize(12).TextColor(t.TextMuted)
						}
						opts := editor.Options{Width: max(100, w-270), Theme: t, Page: pageStyle(v.host.paperIsDark()), FontSize: v.host.preferences().ReadingSize(), LineHeight: v.host.preferences().ReadingLine(), FontFamily: v.host.preferences().BodyFamily(), Column: v.host.preferences().Column(), LoadImage: func(url string) editor.ImageResource { return v.host.imageResourceFor(v.for_, url) }}
						if v.showMarks {
							opts.HighlightBlocks = v.marks
						}
						editor.View(c.Key("history-preview:"+v.shown), v.preview, opts)
						return
					}
					switch {
					case v.err != nil:
						ui.Text(c, i18n.T("无法读取这个版本")+" "+v.err.Error()).TextColor(t.Danger)
						return
					case v.doc == nil:
						return
					case v.added == 0 && v.gone == 0:
						ui.Text(c, i18n.T("与当前内容相同")).FontSize(12).TextColor(t.TextMuted)
					default:
						ui.Text(c, fmt.Sprintf(i18n.T("与当前内容相比：恢复后增加 %d 行、删除 %d 行"), v.added, v.gone)).FontSize(12).TextColor(t.TextMuted)
					}
					pg := pageStyle(v.host.paperIsDark())
					ui.Scroll(c.Key("version-diff:" + v.shown)).Grow(1).MinHeight(0).Children(func() {
						ui.Column(c).Gap(1).Children(func() {
							for i, l := range v.diff {
								if l.Op == history.Same && !nearChange(v.diff, i) {
									continue
								}
								row := ui.Row(c.Key(i)).Padding(3, 8).Radius(4).Gap(8)
								sign, color := " ", t.Text
								switch l.Op {
								case history.Added:
									sign, color = "+", t.Text
									row.Background(pg.Callouts["green"].Fill)
								case history.Removed:
									sign, color = "−", t.TextMuted
									row.Background(pg.Callouts["red"].Fill)
								default:
									color = t.TextMuted
								}
								row.Children(func() {
									ui.Text(c, sign).FontSize(13).TextColor(t.TextMuted).Width(10)
									if len(l.Spans) > 0 {
										uiSpans := make([]ui.Span, 0, len(l.Spans))
										for _, sp := range l.Spans {
											us := ui.Span{
												Text:          sp.Text,
												Color:         color,
												Strikethrough: l.Op == history.Removed,
											}
											if sp.Changed {
												us.Background = versionDiffHighlight(v.host.paperIsDark(), l.Op == history.Added)
												us.Radius = 3
												if l.Op == history.Added {
													us.Weight = 500
												}
											}
											uiSpans = append(uiSpans, us)
										}
										ui.RichText(c.Key("diff-rich:"+strconv.Itoa(i)), uiSpans...).FontSize(13).Grow(1).MinWidth(0)
									} else {
										text := l.Text
										if text == "" {
											text = " "
										}
										line := ui.Text(c, text).FontSize(13).TextColor(color).Grow(1).MinWidth(0)
										if l.Op == history.Removed {
											line.Strikethrough()
										}
									}
								})
							}
						})
					})
				})
			})
		})
	})
	if v.host.window() != nil && hasAppWindowControls() {
		windowButtons(c, v.host.window())
	}
}

// nearChange reports whether a line is within two lines of a change, the
// context the difference shows around changes.
func nearChange(diff []history.Line, i int) bool {
	for j := max(0, i-2); j <= min(len(diff)-1, i+2); j++ {
		if diff[j].Op != history.Same {
			return true
		}
	}
	return false
}
