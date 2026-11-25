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
		ui.Column(c).Width(min(720, w-40)).MaxHeight(h - 60).Gap(10).Children(func() {
			ui.Text(c, i18n.T("清理未使用附件")).FontSize(18).FontWeight(600)
			ui.Text(c, i18n.T("已核查正文、白板、历史版本、回收站和本机文件；移除前会再次检查引用")).FontSize(13).TextColor(t.TextMuted)
			if s.busy {
				ui.Text(c, i18n.T("正在检查附件"))
				c.After(100 * time.Millisecond)
			}
			if s.err != "" {
				ui.Text(c, s.err).TextColor(t.Danger)
			}
			if s.message != "" {
				ui.Text(c, s.message)
			}
			var selected int
			var size int64
			ui.Scroll(c.Key("unused-assets")).MaxHeight(max(100, h-290)).Children(func() {
				for _, it := range s.items {
					ui.Row(c.Key(it.Path)).Gap(8).PaddingY(5).Children(func() {
						checked := s.selected[it.Path]
						ui.Checkbox(c.Key("select"), &checked, it.Path).Disabled(s.busy).Grow(1)
						s.selected[it.Path] = checked
						if checked {
							selected++
							size += it.Bytes
						}
						ui.Text(c, fmt.Sprintf("%.1f MiB", float64(it.Bytes)/(1<<20))).FontSize(12).TextColor(t.TextMuted)
					})
				}
			})
			if !s.busy && len(s.items) == 0 && s.err == "" && s.message == "" {
				ui.Text(c, i18n.T("没有发现未使用附件"))
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("已选择 %d 项，共 %.1f MiB"), selected, float64(size)/(1<<20))).Grow(1)
				if ui.Button(c.Key("cleanup-close"), i18n.T("关闭")).Disabled(s.busy).Clicked() {
					s.open = false
				}
				if ui.Button(c.Key("cleanup-refresh"), i18n.T("重新检查")).Disabled(s.busy).Clicked() {
					a.scanUnusedAssets()
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
