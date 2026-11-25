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
		ui.Column(c).Width(min(760, w-40)).MaxHeight(h - 60).Gap(10).Children(func() {
			ui.Text(c, i18n.T("导入已有笔记")).FontSize(18).FontWeight(600)
			ui.Text(c, i18n.T("复制 Markdown 笔记库、思源工作空间或导出 ZIP；原文件保持不变；转换结果写入导入报告")).FontSize(13).TextColor(t.TextMuted)
			if s.discovering {
				ui.Text(c, i18n.T("正在查找已有笔记库"))
				c.After(100 * time.Millisecond)
			}
			ui.Scroll(c.Key("found-libraries")).MaxHeight(min(160, h/4)).Children(func() {
				for _, found := range s.found {
					label := fmt.Sprintf("%s · %s · %d", found.App, found.Name, found.Documents)
					if ui.Button(c.Key(found.Path), label).Tooltip(found.Path).Disabled(s.busy).Clicked() {
						s.source = found.Path
						s.result = nil
					}
				}
			})
			ui.TextInput(c.Key("import-source"), &s.source).Label(i18n.T("笔记来源路径")).Placeholder(i18n.T("选择笔记文件夹或导出 ZIP")).Disabled(s.busy)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c.Key("import-folder"), i18n.T("选择文件夹")).Disabled(s.busy).Clicked() {
					a.chooseImportSource(true)
				}
				if ui.Button(c.Key("import-file"), i18n.T("选择文件或 ZIP")).Disabled(s.busy).Clicked() {
					a.chooseImportSource(false)
				}
			})
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
			before := selected
			ui.Select(c.Key("import-target"), &selected, labels).Label(i18n.T("导入目标空间")).Disabled(s.busy)
			if selected != before {
				for i, label := range labels {
					if label == selected {
						s.spaceID = ids[i]
						break
					}
				}
			}
			if s.err != "" {
				ui.Text(c, s.err).TextColor(t.Danger)
			}
			if s.busy {
				ui.Text(c, i18n.T("正在复制和转换笔记"))
				c.After(100 * time.Millisecond)
			}
			if s.result != nil {
				r := s.result
				ui.Text(c, fmt.Sprintf(i18n.T("已导入 %d 篇文档、%d 个附件，%d 项需要查看报告"), r.Documents, r.Attachments, len(r.Issues)))
				ui.Scroll(c.Key("import-report")).MaxHeight(max(80, h-500)).Children(func() {
					for i, issue := range r.Issues {
						ui.Text(c.Key(i), issue.File+" · "+issue.Detail).FontSize(13)
					}
				})
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
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
