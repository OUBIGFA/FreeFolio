package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/assetcleanup"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

type cleanupState struct {
	open, busy, confirmed bool
	spaceID               string
	items                 []assetcleanup.Candidate
	selected              map[string]bool
	err, message          string
}

func (a *App) cleanupLive() ([]string, error) {
	var live []string
	for _, d := range a.residentDocuments() {
		if d.board != nil {
			s, e := d.board.Scene.Encode()
			if e != nil {
				return nil, e
			}
			live = append(live, s)
		}
		if d.ed != nil {
			live = append(live, editor.ExportMarkdown(d.ed.Committed()))
		}
		if d.source {
			live = append(live, d.preview)
		}
	}
	return live, nil
}
func (a *App) cleanupRequest(sp *space.Space) (assetcleanup.Request, error) {
	live, err := a.cleanupLive()
	if err != nil {
		return assetcleanup.Request{}, err
	}
	r := assetcleanup.Request{Root: sp.Root, AssetDir: sp.AttachmentsDir, Live: live, ReferenceRoots: []string{a.bin.Dir, filepath.Join(a.cfg.ConfigDir, "sync-state", "backups")}}
	for _, other := range a.state.Spaces {
		if other.Root != sp.Root && !other.IsExternal() {
			r.ReferenceRoots = append(r.ReferenceRoots, other.Root)
		}
	}
	for _, d := range a.residentDocuments() {
		if d.space.IsExternal() {
			r.ReferenceFiles = append(r.ReferenceFiles, d.fullPath())
		}
	}
	return r, nil
}

// Saved loose-file references are read on the worker, never in View.
func (a *App) scanCleanupRequest(req assetcleanup.Request) ([]assetcleanup.Candidate, error) {
	data, e := os.ReadFile(filepath.Join(a.cfg.ConfigDir, "file-places.json"))
	if e == nil {
		var places map[string]filePlace
		if e = json.Unmarshal(data, &places); e != nil {
			return nil, e
		}
		for p := range places {
			req.ReferenceFiles = append(req.ReferenceFiles, p)
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	return assetcleanup.Scan(req)
}
func (a *App) scanUnusedAssets() {
	if a.cleanup.busy {
		return
	}
	sp := a.state.ActiveSpace()
	if sp == nil || sp.IsExternal() {
		return
	}
	req, err := a.cleanupRequest(sp)
	if err != nil {
		a.cleanup = cleanupState{open: true, err: err.Error()}
		return
	}
	a.cleanup = cleanupState{open: true, busy: true, spaceID: sp.ID, selected: map[string]bool{}}
	a.backgroundResult(func() func() {
		items, e := a.scanCleanupRequest(req)
		return func() {
			a.cleanup.busy = false
			a.cleanup.items = items
			if e != nil {
				a.cleanup.err = e.Error()
				return
			}
			for _, it := range items {
				a.cleanup.selected[it.Path] = true
			}
		}
	})
}
func (a *App) recycleUnusedAssets() {
	s := &a.cleanup
	if s.busy {
		return
	}
	sp := a.state.Get(s.spaceID)
	if sp == nil {
		return
	}
	snapshot := *sp
	req, err := a.cleanupRequest(sp)
	if err != nil {
		s.err = err.Error()
		return
	}
	chosen := map[string]assetcleanup.Candidate{}
	for _, it := range s.items {
		if s.selected[it.Path] {
			chosen[it.Path] = it
		}
	}
	s.busy = true
	s.err = ""
	s.confirmed = false
	bin := a.bin
	trash := a.trashItem
	win := a.win
	// Detached editors may change while scanning. Their latest text is checked
	// on the UI thread immediately before a confirmed attachment is recycled.
	stillUnused := func(name string) bool {
		check := func() bool {
			live, err := a.cleanupLive()
			if err != nil {
				return false
			}
			for _, text := range live {
				if assetcleanup.References(text, name) {
					return false
				}
			}
			return true
		}
		if win == nil {
			return check()
		}
		answer := make(chan bool, 1)
		win.Update(func() { answer <- check() })
		return <-answer
	}
	a.backgroundResult(func() func() {
		fresh, e := a.scanCleanupRequest(req)
		count := 0
		if e == nil {
			for _, it := range fresh {
				old, ok := chosen[it.Path]
				if !ok || old.Hash != it.Hash || old.Bytes != it.Bytes {
					continue
				}
				if !stillUnused(filepath.Base(it.Path)) {
					continue
				}
				full, err := vault.Resolve(snapshot.Root, it.Path)
				if err != nil {
					e = err
					break
				}
				if _, err = bin.Add(snapshot.ID, snapshot.Name, snapshot.Root, it.Path, time.Now()); err != nil {
					e = err
					break
				}
				if same, err := assetcleanup.Unchanged(snapshot.Root, it); err != nil {
					e = err
					break
				} else if !same {
					continue
				}
				if err = trash(full); err != nil {
					e = err
					break
				}
				count++
			}
		}
		return func() {
			s.busy = false
			s.items = nil
			if e != nil {
				s.err = e.Error()
			}
			s.message = fmt.Sprintf(i18n.T("已将 %d 个附件移到回收站，并保留恢复副本"), count)
			a.invalidateResolvedImages()
		}
	})
}
func (a *App) cleanupView(c *ui.Context) {
	s := &a.cleanup
	if !s.open {
		return
	}
	w, h := c.Size()
	t := c.Theme()
	ui.Modal(c.Key("asset-cleanup"), &s.open, func() {
		ui.Column(c).Width(min(700, w-40)).MaxHeight(h - 56).Gap(20).Children(func() {
			ui.Row(c.Key("cleanup-header")).Gap(20).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(24, 24).Shrink(0).Center().Children(func() {
					ui.Icon(c, iconTrash).FontSize(18).TextColor(t.Warning)
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Text(c, i18n.T("清理未使用附件")).FontSize(17).FontWeight(600)
						if sp := a.state.Get(s.spaceID); sp != nil {
							ui.Row(c).Padding(4, 12).Radius(8).Background(t.Surface).Shrink(0).Children(func() {
								ui.Text(c, sp.Name).FontSize(11).FontWeight(500).TextColor(t.TextMuted).SingleLine()
							})
						}
					})
					ui.Text(c, i18n.T("已核查正文、白板、历史版本、回收站和本机文件，移除前会再次检查引用")).FontSize(12.5).TextColor(t.TextMuted).LineHeight(1.45)
				})
			})
			if s.err != "" {
				ui.Row(c.Key("cleanup-error")).Padding(16, 20).Gap(12).AlignItems(ui.Center).
					Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.35)).Radius(8).Children(func() {
					ui.Icon(c, iconAlert).FontSize(15).TextColor(t.Danger).Shrink(0)
					ui.Text(c, s.err).FontSize(12.5).TextColor(t.Danger).Grow(1).MinWidth(0).LineHeight(1.45)
				})
			}
			if s.message != "" {
				ui.Row(c.Key("cleanup-message")).Padding(16, 20).Gap(12).AlignItems(ui.Center).
					Background(t.Success.Alpha(0.08)).Border(1, t.Success.Alpha(0.35)).Radius(8).Children(func() {
					ui.Icon(c, iconCheck).FontSize(15).TextColor(t.Success).Shrink(0)
					ui.Text(c, s.message).FontSize(13).Grow(1).MinWidth(0).LineHeight(1.45)
				})
			}
			var selected int
			var size int64
			for _, it := range s.items {
				if s.selected[it.Path] {
					selected++
					size += it.Bytes
				}
			}
			ui.Column(c.Key("cleanup-card")).Background(t.Surface).Radius(panelRadius).Clip().Children(func() {
				if s.busy {
					ui.Column(c.Key("cleanup-busy")).Padding(56, 32).Gap(16).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c.Key("cleanup-spinner")).TextColor(t.Accent).Label(i18n.T("正在检查附件"))
						ui.Text(c, i18n.T("正在检查附件")).FontSize(13).TextColor(t.TextMuted)
					})
					c.After(100 * time.Millisecond)
					return
				}
				if len(s.items) == 0 {
					ui.Column(c.Key("cleanup-empty")).Padding(48, 32).Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(24, 24).Center().Children(func() {
							ui.Icon(c, iconCheck).FontSize(18).TextColor(t.Success)
						})
						ui.Text(c, i18n.T("没有发现未使用附件")).FontSize(14).FontWeight(600)
						ui.Text(c, i18n.T("当前空间的所有附件均已被引用，或已清理完成")).FontSize(12).TextColor(t.TextMuted)
					})
					return
				}
				allChecked := selected == len(s.items)
				beforeAll := allChecked
				ui.Row(c.Key("cleanup-toolbar")).Padding(12, 20).Gap(12).AlignItems(ui.Center).Background(t.Background.Alpha(0.55)).Children(func() {
					ui.Checkbox(c.Key("cleanup-select-all"), &allChecked, fmt.Sprintf(i18n.T("全选（已选 %d / %d 项）"), selected, len(s.items))).Grow(1)
					ui.Text(c, byteSize(size)).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
				})
				if allChecked != beforeAll {
					for _, it := range s.items {
						s.selected[it.Path] = allChecked
					}
					s.confirmed = false
				}
				ui.Scroll(c.Key("unused-assets")).MaxHeight(max(120, h-380)).Children(func() {
					for i, it := range s.items {
						if i > 0 {
							settingsSeparator(c)
						}
						ui.Row(c.Key(it.Path)).Padding(12, 20).Gap(16).AlignItems(ui.Center).Children(func() {
							checked := s.selected[it.Path]
							before := checked
							ui.Checkbox(c.Key("select"), &checked, "").Label(it.Path).Disabled(s.busy)
							if checked != before {
								s.selected[it.Path] = checked
								s.confirmed = false
							}
							ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
								ui.Text(c, filepath.Base(it.Path)).FontSize(13).FontWeight(500).SingleLine()
								if dir := filepath.Dir(it.Path); dir != "." && dir != "" {
									ui.Text(c, filepath.ToSlash(dir)).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
								}
							})
							ui.Text(c, byteSize(it.Bytes)).FontSize(12).TextColor(t.TextMuted).Shrink(0)
						})
					}
				})
			})
			if s.confirmed && selected > 0 && !s.busy {
				ui.Row(c.Key("cleanup-confirm-bar")).Padding(16, 20).Gap(16).AlignItems(ui.Center).
					Background(t.Warning.Alpha(0.10)).Border(1, t.Warning.Alpha(0.35)).Radius(8).Children(func() {
					ui.Icon(c, iconAlert).FontSize(15).TextColor(t.Warning).Shrink(0)
					ui.Text(c, fmt.Sprintf(i18n.T("即将把 %d 个附件（%s）移到系统回收站，并保留恢复副本"), selected, byteSize(size))).
						FontSize(12.5).Grow(1).MinWidth(0).LineHeight(1.4)
					if ui.Button(c.Key("cleanup-cancel-confirm"), i18n.T("取消")).Clicked() {
						s.confirmed = false
					}
				})
			}
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				if ui.Button(c.Key("cleanup-refresh"), i18n.T("重新检查")).Disabled(s.busy).Clicked() {
					a.scanUnusedAssets()
				}
				ui.Spacer(c)
				if ui.Button(c.Key("cleanup-close"), i18n.T("关闭")).Disabled(s.busy).Clicked() {
					s.open = false
				}
				label := i18n.T("移到回收站")
				if s.confirmed {
					label = i18n.T("确认移到回收站")
				}
				if ui.PrimaryButton(c.Key("cleanup-recycle"), label).Disabled(s.busy || selected == 0).Clicked() {
					if s.confirmed {
						a.recycleUnusedAssets()
					} else {
						s.confirmed = true
					}
				}
			})
		})
	})
	if !s.open {
		s.confirmed = false
	}
}
