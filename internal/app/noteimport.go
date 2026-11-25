package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/noteimport"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type noteImportState struct {
	open, busy, discovering, offer bool
	found                          []noteimport.Found
	source, spaceID, err           string
	result                         *noteimport.Report
}

func (a *App) discoverLibraries() {
	if a.imports.discovering {
		return
	}
	a.imports.discovering = true
	a.backgroundResult(func() func() {
		home, e := os.UserHomeDir()
		config, ce := os.UserConfigDir()
		var found []noteimport.Found
		if e == nil && ce == nil {
			found = noteimport.Discover(home, config)
		}
		return func() {
			a.imports.discovering = false
			a.imports.found = found
			if e != nil {
				a.imports.err = e.Error()
			} else if ce != nil {
				a.imports.err = ce.Error()
			}
		}
	})
}
func (a *App) openImportGuide() {
	a.imports.open = true
	a.imports.offer = false
	if sp := a.state.ActiveSpace(); sp != nil && !sp.IsExternal() {
		a.imports.spaceID = sp.ID
	}
	a.discoverLibraries()
}
func (a *App) chooseImportSource(folder bool) {
	if a.win == nil {
		return
	}
	win := a.win
	title := i18n.T("选择笔记来源")
	go func() {
		files, e := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: title, Directory: folder})
		win.Update(func() {
			if e != nil {
				a.imports.err = e.Error()
			} else if len(files) > 0 {
				a.imports.source = files[0]
				a.imports.result = nil
			}
		})
	}()
}
func (a *App) runNoteImport() {
	s := &a.imports
	if s.busy {
		return
	}
	sp := a.state.Get(s.spaceID)
	if sp == nil || sp.IsExternal() {
		s.err = i18n.T("请选择导入目标空间")
		return
	}
	src := strings.TrimSpace(s.source)
	if src == "" {
		return
	}
	root := sp.Root
	s.busy = true
	s.err = ""
	s.result = nil
	a.backgroundResult(func() func() {
		r, e := noteimport.ImportWith(src, root, editor.CanonicalizeImportedNote)
		return func() {
			s.busy = false
			s.result = &r
			if e != nil {
				s.err = e.Error()
			}
			a.refreshTree()
			a.refreshSearch()
		}
	})
}
func (a *App) importGuideView(c *ui.Context) {
	s := &a.imports
	if s.offer && !a.tour.open && a.win != nil {
		s.offer = false
		s.open = true
	}
	if !s.open {
		return
	}
	w, h := c.Size()
	t := c.Theme()
	ui.Modal(c.Key("note-import"), &s.open, func() {
		ui.Column(c).Width(min(700, w-40)).MaxHeight(h - 56).Gap(14).Children(func() {
			ui.Row(c.Key("import-header")).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(36, 36).Radius(8).Shrink(0).Center().Background(t.Accent.Alpha(0.10)).Children(func() {
					ui.Icon(c, iconFileImport).FontSize(18).TextColor(t.Accent)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, i18n.T("导入已有笔记")).FontSize(17).FontWeight(600)
					ui.Text(c, i18n.T("复制 Markdown 笔记库、思源工作空间或导出 ZIP，原文件保持不变")).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.45)
				})
			})
			ui.Scroll(c.Key("import-body")).Grow(1).MinHeight(0).Gap(12).Children(func() {
				ui.Column(c.Key("import-source-card")).Background(t.Surface).Border(1, t.Border).Radius(panelRadius).Clip().Children(func() {
					ui.Row(c).Padding(10, 14).AlignItems(ui.Center).Children(func() {
						ui.Text(c, i18n.T("来源位置")).FontSize(12.5).FontWeight(600).Grow(1)
						if s.discovering {
							ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Spinner(c.Key("import-discover-spinner")).TextColor(t.Accent).Label(i18n.T("正在查找已有笔记库"))
								ui.Text(c, i18n.T("正在查找已有笔记库")).FontSize(12).TextColor(t.TextMuted)
							})
							c.After(100 * time.Millisecond)
						}
					})
					ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
					if len(s.found) > 0 {
						ui.Scroll(c.Key("found-libraries")).MaxHeight(min(156, h/4)).Padding(8, 10).Gap(4).Children(func() {
							for _, found := range s.found {
								selected := s.source == found.Path
								row := ui.ButtonBase(c.Key(found.Path)).WidthPercent(100).Padding(8, 10).Radius(6).Gap(8).
									AlignItems(ui.Center).Focusable().Cursor(ui.CursorPointer).
									Label(fmt.Sprintf("%s · %s · %d", found.App, found.Name, found.Documents)).Tooltip(found.Path).Disabled(s.busy)
								if selected {
									row.Background(t.Accent.Alpha(0.10)).Border(1, t.Accent.Alpha(0.35))
								} else if row.Hovered() {
									row.Background(hoverFill(t))
								}
								row.Children(func() {
									ui.Row(c).Padding(2, 7).Radius(4).Background(t.Background).Border(1, t.Border.Alpha(0.7)).Shrink(0).Children(func() {
										ui.Text(c, found.App).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine()
									})
									ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
										ui.Text(c, found.Name).FontSize(13).FontWeight(500).SingleLine()
										ui.Text(c, shortPath(found.Path)).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
									})
									ui.Text(c, fmt.Sprintf(i18n.T("%d 篇"), found.Documents)).FontSize(12).TextColor(t.TextMuted).Shrink(0)
									if selected {
										ui.Icon(c, iconCheck).FontSize(14).TextColor(t.Accent).Shrink(0)
									}
								})
								if row.Clicked() {
									s.source = found.Path
									s.result = nil
								}
							}
						})
					} else if !s.discovering {
						ui.Row(c).Padding(10, 14).Children(func() {
							ui.Text(c, i18n.T("未自动发现已有笔记库，请手动选择文件夹或导出 ZIP")).FontSize(12).TextColor(t.TextMuted)
						})
					}
					ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
					ui.Column(c).Padding(12, 14).Gap(8).Children(func() {
						ui.Text(c, i18n.T("笔记来源路径")).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.TextInput(c.Key("import-source"), &s.source).Label(i18n.T("笔记来源路径")).
								Placeholder(i18n.T("选择笔记文件夹或导出 ZIP")).Disabled(s.busy).Grow(1).MinWidth(0)
							if ui.Button(c.Key("import-folder"), i18n.T("选择文件夹")).Disabled(s.busy).Shrink(0).Clicked() {
								a.chooseImportSource(true)
							}
							if ui.Button(c.Key("import-file"), i18n.T("选择文件或 ZIP")).Disabled(s.busy).Shrink(0).Clicked() {
								a.chooseImportSource(false)
							}
						})
					})
				})

				ui.Column(c.Key("import-target-card")).Background(t.Surface).Border(1, t.Border).Radius(panelRadius).Clip().Children(func() {
					var labels, ids []string
					selected := ""
					for _, sp := range a.state.Spaces {
						if !sp.IsExternal() {
							label := sp.Name + " · " + shortPath(sp.Root)
							labels = append(labels, label)
							ids = append(ids, sp.ID)
							if sp.ID == s.spaceID {
								selected = label
							}
						}
					}
					settingRow(c, "import-target-row", i18n.T("导入目标空间"), i18n.T("在目标空间内新建文件夹，不覆盖已有笔记"), func() {
						before := selected
						ui.Select(c.Key("import-target"), &selected, labels).Label(i18n.T("导入目标空间")).Width(240).Disabled(s.busy)
						if selected != before {
							for i, label := range labels {
								if label == selected {
									s.spaceID = ids[i]
									break
								}
							}
						}
					})
				})

				if s.busy {
					ui.Row(c.Key("import-busy")).Padding(12, 14).Gap(10).AlignItems(ui.Center).
						Background(t.Accent.Alpha(0.08)).Border(1, t.Accent.Alpha(0.3)).Radius(8).Children(func() {
						ui.Spinner(c.Key("import-spinner")).TextColor(t.Accent).Label(i18n.T("正在复制和转换笔记"))
						ui.Text(c, i18n.T("正在复制和转换笔记")).FontSize(13).FontWeight(500)
					})
					c.After(100 * time.Millisecond)
				}
				if s.err != "" {
					ui.Row(c.Key("import-error")).Padding(10, 14).Gap(8).AlignItems(ui.Center).
						Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.35)).Radius(8).Children(func() {
						ui.Icon(c, iconAlert).FontSize(15).TextColor(t.Danger).Shrink(0)
						ui.Text(c, s.err).FontSize(12.5).TextColor(t.Danger).Grow(1).MinWidth(0).LineHeight(1.45)
					})
				}
				if s.result != nil {
					r := s.result
					ui.Column(c.Key("import-result")).Background(t.Surface).Border(1, t.Border).Radius(panelRadius).Clip().Children(func() {
						ui.Row(c).Padding(12, 14).Gap(10).AlignItems(ui.Center).Background(t.Success.Alpha(0.08)).Children(func() {
							ui.Icon(c, iconCheck).FontSize(15).TextColor(t.Success).Shrink(0)
							ui.Text(c, fmt.Sprintf(i18n.T("已导入 %d 篇文档、%d 个附件，%d 项需要查看报告"), r.Documents, r.Attachments, len(r.Issues))).FontSize(13).FontWeight(600).Grow(1)
						})
						ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
						ui.Row(c).Padding(10, 14).Gap(10).Children(func() {
							for _, stat := range []struct {
								label string
								count int
							}{{i18n.T("已导入文档"), r.Documents}, {i18n.T("已复制附件"), r.Attachments}, {i18n.T("待查看事项"), len(r.Issues)}} {
								ui.Column(c).Grow(1).Padding(8, 10).Radius(6).Background(t.Background).Border(1, t.Border.Alpha(0.6)).Gap(2).Children(func() {
									ui.Text(c, fmt.Sprintf("%d", stat.count)).FontSize(16).FontWeight(600)
									ui.Text(c, stat.label).FontSize(11.5).TextColor(t.TextMuted)
								})
							}
						})
						if len(r.Issues) > 0 {
							ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
							ui.Scroll(c.Key("import-report")).MaxHeight(min(140, max(80, h-500))).Padding(8, 14).Gap(6).Children(func() {
								for i, issue := range r.Issues {
									ui.Column(c.Key(i)).Gap(1).Children(func() {
										ui.Text(c, issue.File).FontSize(12).FontWeight(500).SingleLine()
										ui.Text(c, issue.Detail).FontSize(11.5).TextColor(t.TextMuted).LineHeight(1.4)
									})
								}
							})
						}
					})
				}
			})
			ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("转换结果会写入导入报告，原文件保持不变")).FontSize(12).TextColor(t.TextMuted).Grow(1).SingleLine()
				if ui.Button(c.Key("import-close"), i18n.T("关闭")).Disabled(s.busy).Clicked() {
					s.open = false
				}
				if s.result != nil && s.result.Folder != "" {
					if ui.Button(c.Key("import-open"), i18n.T("打开导入报告")).Clicked() {
						if sp := a.state.Get(s.spaceID); sp != nil {
							a.settingsOpen = false
							s.open = false
							a.openIn(sp, s.result.ReportPath)
						}
					}
				}
				if ui.PrimaryButton(c.Key("import-confirm"), i18n.T("复制并导入")).Disabled(s.busy || strings.TrimSpace(s.source) == "" || s.spaceID == "").Clicked() {
					a.runNoteImport()
				}
			})
		})
	})
}
