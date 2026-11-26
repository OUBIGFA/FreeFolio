package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/cloudsync"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/OUBIGFA/FreeFolio/internal/trashbin"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type cloudGuard struct {
	space, rel string
	reply      chan error
}
type cloudOutcome struct {
	result    cloudsync.Result
	err       error
	probe     bool
	conflicts []cloudConflict
	recovery  *cloudRecoveryTarget
	onlySpace string
}

// Recovery is bound to the failed space and destination, so editing settings
// cannot turn an old failure into authorization to overwrite another folder.
type cloudRecoveryTarget struct {
	space, name, root, remote, target string
}
type cloudConflict struct {
	space, name string
	detail      cloudsync.Conflict
}
type cloudProgress struct {
	space  string
	detail cloudsync.Progress
}
type cloudState struct {
	config          cloudsync.Config
	broken          bool
	message         string
	failed, busy    bool
	cancel          context.CancelFunc
	done            chan cloudOutcome
	guards          chan cloudGuard
	next, due, last time.Time
	day             string
	count, retries  int
	started         bool
	conflicts       []cloudConflict
	resolve         map[string]map[string]cloudsync.Resolution
	progress        *atomic.Pointer[cloudProgress]
	lastError       error
	recheck         bool
	recoveryFocus   string

	recovery                    *cloudRecoveryTarget
	reconcile, confirmReconcile string
}

func (a *App) loadCloudSync() {
	c, err := cloudsync.LoadConfig(a.cfg.ConfigDir)
	a.applyLoadedCloudSync(c, err)
}

func (a *App) applyLoadedCloudSync(c cloudsync.Config, err error) {
	a.cloud.config = c
	if err != nil {
		a.cloud.broken = true
		a.cloud.failed = true
		a.cloud.message = fmt.Sprintf(i18n.T("读取同步设置失败：%v"), err)
	}
}

func (a *App) saveCloudSync() bool {
	if a.cloud.broken {
		a.cloud.message = i18n.T("同步设置无法读取，原文件已保留")
		return false
	}
	if a.cloud.busy {
		return false
	}
	if err := cloudsync.SaveConfig(a.cfg.ConfigDir, a.cloud.config); err != nil {
		a.cloud.failed = true
		a.cloud.message = err.Error()
		return false
	}
	a.cloud.failed = false
	a.cloud.message = i18n.T("同步设置已保存")
	a.cloud.next = a.cloud.config.Next(a.now)
	return true
}

func (a *App) startCloudSync(probe, manual bool) {
	s := &a.cloud
	// Consume recovery before any early return. A canceled save or failed
	// preflight must never leave an overwrite queued for an automatic run.
	reconcile := s.reconcile
	s.reconcile, s.confirmReconcile = "", ""
	if s.busy || s.broken {
		return
	}
	if reconcile != "" && (probe || !manual || !a.cloudRecoveryValid()) {
		s.failed, s.message = true, i18n.T("同步目标已改变，请先重新同步")
		return
	}
	if !probe && (!s.config.Enabled || !a.flushForTask()) {
		return
	}
	c := s.config
	c.Spaces = maps.Clone(c.Spaces)
	if reconcile != "" {
		c.Spaces = map[string]string{s.recovery.space: s.recovery.remote}
	}
	var spaces []space.Space
	for id := range c.Spaces {
		if sp := a.state.Get(id); sp != nil && !sp.IsExternal() {
			spaces = append(spaces, *sp)
		}
	}
	slices.SortFunc(spaces, func(a, b space.Space) int { return strings.Compare(a.ID, b.ID) })
	if !probe && len(spaces) == 0 {
		s.failed = true
		s.message = i18n.T("请至少选择一个同步空间")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	s.cancel = cancel
	s.busy = true
	s.failed = false
	s.message = i18n.T("正在同步")
	if probe {
		s.message = i18n.T("正在测试连接")
	}
	s.done = make(chan cloudOutcome, 1)
	s.guards = make(chan cloudGuard)
	s.progress = &atomic.Pointer[cloudProgress]{}
	done, guards := s.done, s.guards
	progress := s.progress
	resolutions := s.resolve
	s.resolve = nil
	recheck := s.recheck
	s.recheck = false
	stateDir := filepath.Join(a.cfg.ConfigDir, "sync-state")
	bin := trashbin.Bin{Dir: a.bin.Dir}
	trash := a.trashFile
	if trash == nil {
		trash = mygo.Shell.TrashItem
	}
	if !probe && !manual {
		s.count++
	}
	if !probe {
		// A manual run also satisfies startup sync; do not queue it again on completion.
		s.started = true
	}
	go func() {
		defer cancel()
		out := cloudOutcome{probe: probe}
		if reconcile != "" {
			out.onlySpace = spaces[0].ID
		}
		if probe {
			out.err = cloudsync.TestConnection(ctx, c)
			done <- out
			return
		}
		for _, sp := range spaces {
			hooks := cloudsync.Hooks{
				Reconcile: reconcile,
				Recheck:   recheck || len(resolutions[sp.ID]) > 0,
				Merge:     editor.MergeFiles,
				// Keep the latest progress without making the worker wait for a frame.
				Progress: func(p cloudsync.Progress) { progress.Store(&cloudProgress{sp.Name, p}) },
				Resolve:  resolutions[sp.ID],
				BeforeApply: func(rel string) error {
					reply := make(chan error, 1)
					select {
					case guards <- cloudGuard{sp.ID, rel, reply}:
					case <-ctx.Done():
						return ctx.Err()
					}
					select {
					case err := <-reply:
						return err
					case <-ctx.Done():
						return ctx.Err()
					}
				},
				Trash: func(full string) error {
					rel, err := filepath.Rel(sp.Root, full)
					if err != nil {
						return err
					}
					if _, err = bin.Add(sp.ID, sp.Name, sp.Root, filepath.ToSlash(rel), time.Now()); err != nil {
						return err
					}
					return trash(full)
				},
			}
			r, err := cloudsync.Sync(ctx, c, cloudsync.Space{ID: sp.ID, Root: sp.Root, Remote: c.Spaces[sp.ID]}, stateDir, hooks)
			out.result.Uploaded += r.Uploaded
			out.result.Downloaded += r.Downloaded
			out.result.Deleted += r.Deleted
			out.result.Merged += r.Merged
			for _, detail := range r.Details {
				out.conflicts = append(out.conflicts, cloudConflict{sp.ID, sp.Name, detail})
			}
			for _, rel := range r.Conflicts {
				out.result.Conflicts = append(out.result.Conflicts, sp.Name+" / "+rel)
			}
			for _, rel := range r.Skipped {
				out.result.Skipped = append(out.result.Skipped, sp.Name+" / "+rel)
			}
			if err != nil {
				out.err = fmt.Errorf("%s: %w", sp.Name, err)
				out.recovery = &cloudRecoveryTarget{sp.ID, sp.Name, sp.Root, c.Spaces[sp.ID], c.TargetID()}
				break
			}
		}
		done <- out
	}()
}

// cloudTick is the only consumer of worker results. Active editor state is
// checked here, on the UI thread, before a worker replaces a local file.
func (a *App) cloudTick(c *ui.Context) {
	s := &a.cloud
	now := c.Now()
	if !s.started {
		// Startup is a one-time launch decision, even when automatic sync is off.
		s.started = true
		if s.config.Enabled && s.config.Automatic && !s.broken && s.config.SyncOnStart {
			s.due = now.Add(2 * time.Second)
		}
	}
	if !s.config.Enabled || !s.config.Automatic || s.broken {
		// Turning sync off cancels pending work, so enabling it cannot replay
		// startup, save, retry or overdue scheduled jobs from before the pause.
		s.next = time.Time{}
		s.due = time.Time{}
		s.retries = 0
	}
	if s.busy {
		if s.progress != nil {
			if p := s.progress.Swap(nil); p != nil {
				phase := i18n.T("正在检查文件")
				switch p.detail.Phase {
				case "local":
					phase = i18n.T("正在扫描本地文件")
				case "list":
					phase = i18n.T("正在检查云端目录")
				case "verify":
					phase = i18n.T("正在核对云端文件")
				case "upload":
					phase = i18n.T("正在上传文件")
				case "download":
					phase = i18n.T("正在应用云端更改")
				case "merge":
					phase = i18n.T("正在合并文档改动")
				case "delete":
					phase = i18n.T("正在同步删除")
				}
				s.message = phase + " · " + p.space
				if p.detail.Total > 0 {
					s.message += fmt.Sprintf(" · %d/%d", p.detail.Checked, p.detail.Total)
				}
				if p.detail.Path != "" {
					s.message += "\n" + p.detail.Path
				}
			}
		}
		select {
		case g := <-s.guards:
			var err error
			if d := a.residentDocument(g.space, g.rel); d != nil && (d.saving || d.dirty() || d.ed.Composing() || d.conflict || d.ed.ReadOnly) {
				err = errors.New("document has active or protected edits")
			}
			g.reply <- err
		case out := <-s.done:
			s.lastError = out.err
			s.recovery, s.confirmReconcile = out.recovery, ""
			if !out.probe {
				if out.onlySpace == "" {
					s.conflicts = out.conflicts
				} else {
					s.conflicts = slices.DeleteFunc(s.conflicts, func(conflict cloudConflict) bool { return conflict.space == out.onlySpace })
					s.conflicts = append(s.conflicts, out.conflicts...)
				}
			}
			s.busy = false
			s.cancel = nil
			s.progress = nil
			if !out.probe && out.err == nil {
				s.last = now
			}
			s.next = s.config.Next(now)
			s.due = time.Time{}
			canceled := errors.Is(out.err, context.Canceled)
			s.failed = out.err != nil && !canceled
			if canceled {
				s.message = i18n.T("已取消同步")
				s.retries = 0
			} else if out.err != nil {
				s.message = fmt.Sprintf(i18n.T("同步失败：%v"), errText(out.err))
				var networkError net.Error
				if errors.Is(out.err, context.DeadlineExceeded) || errors.As(out.err, &networkError) && networkError.Timeout() {
					s.message = fmt.Sprintf(i18n.T("同步超时，请检查网络后重试：%v"), out.err)
				}
				// Version conflicts and rejected credentials need a user decision,
				// not another scheduled attempt with the same cached state.
				var httpError *cloudsync.HTTPError
				needsAction := errors.Is(out.err, cloudsync.ErrRemoteChanged) || errors.As(out.err, &httpError) && (httpError.Status < 500 && httpError.Status != http.StatusTooManyRequests || httpError.Status == http.StatusInsufficientStorage)
				if !needsAction && s.config.Automatic && s.retries < s.config.RetryLimit {
					s.retries++
					s.due = now.Add(time.Duration(s.retries) * time.Minute)
				}
			} else if out.probe {
				s.message = i18n.T("连接测试通过")
				s.retries = 0
			} else {
				r := out.result
				s.retries = 0
				s.message = fmt.Sprintf(i18n.T("同步完成：上传 %d，下载 %d，删除 %d，冲突 %d"), r.Uploaded, r.Downloaded, r.Deleted, len(r.Conflicts))
				if r.Merged > 0 {
					s.message += fmt.Sprintf(i18n.T("；已合并 %d 篇文档"), r.Merged)
				}
				if r.Uploaded == 0 && r.Downloaded == 0 && r.Deleted == 0 && len(r.Conflicts) == 0 && len(r.Skipped) == 0 {
					s.message = i18n.T("无需同步：本地与云端内容一致")
				}
				if len(r.Conflicts) > 0 {
					s.message += "\n" + i18n.T("已保留冲突副本：") + strings.Join(r.Conflicts, "、")
				}
				if len(r.Skipped) > 0 {
					s.message += "\n" + i18n.T("已跳过正在编辑或只读的文件：") + strings.Join(r.Skipped, "、")
				}
			}
			if !out.probe && (out.result.Downloaded > 0 || out.result.Deleted > 0 || len(out.result.Conflicts) > 0) {
				a.invalidateResolvedImages()
				a.refreshTree()
				a.refreshSearch()
				for _, d := range a.residentDocuments() {
					a.withDocument(d, a.checkDisk)
				}
			}
		default:
		}
		c.After(50 * time.Millisecond)
		return
	}
	if !s.config.Enabled || !s.config.Automatic || s.broken {
		return
	}
	if day := now.Format("2006-01-02"); day != s.day {
		s.day = day
		s.count = 0
	}
	if s.next.IsZero() {
		s.next = s.config.Next(now)
	}
	if s.config.DailyLimit > 0 && s.count >= s.config.DailyLimit {
		c.After(time.Minute)
		return
	}
	due := s.next
	if !s.due.IsZero() && (due.IsZero() || s.due.Before(due)) {
		due = s.due
	}
	if !due.IsZero() && !now.Before(due) {
		a.startCloudSync(false, false)
		c.After(50 * time.Millisecond)
		return
	}
	if !due.IsZero() {
		c.After(min(time.Minute, max(time.Millisecond, due.Sub(now))))
	}
}

func (a *App) cloudSaved(d *document) {
	if !d.justSaved {
		return
	}
	d.justSaved = false
	if a.cloud.config.Enabled && a.cloud.config.Automatic && a.cloud.config.Spaces[d.space.ID] != "" {
		a.cloud.due = a.now.Add(15 * time.Second)
	}
}

func (a *App) cancelCloudSync() {
	if a.cloud.cancel != nil {
		a.cloud.cancel()
	}
}

func (a *App) cloudRecoveryValid() bool {
	s := &a.cloud
	r := s.recovery
	if r == nil || r.target != s.config.TargetID() || s.config.Spaces[r.space] != r.remote {
		return false
	}
	sp := a.state.Get(r.space)
	return sp != nil && !sp.IsExternal() && sp.Root == r.root
}

func (a *App) cloudOverwriteChoices(c *ui.Context) {
	s := &a.cloud
	if s.recovery == nil {
		return
	}
	valid := a.cloudRecoveryValid() && s.config.Enabled && !s.broken
	ui.Text(c, fmt.Sprintf(i18n.T("覆盖范围：%s，单侧文件保留并补齐"), s.recovery.name)).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).Wrap().Children(func() {
		if ui.Button(c.Key("prefer-remote"), i18n.T("以云端为准")).Disabled(!valid).Clicked() {
			s.confirmReconcile = "remote"
		}
		if ui.Button(c.Key("prefer-local"), i18n.T("以本地为准")).Disabled(!valid).Clicked() {
			s.confirmReconcile = "local"
		}
	})
	if s.confirmReconcile == "" {
		return
	}
	hint := i18n.T("将用云端同名文件覆盖本地版本，覆盖前保留恢复副本")
	if s.confirmReconcile == "local" {
		hint = i18n.T("将用本地同名文件覆盖云端版本，覆盖前保留恢复副本")
	}
	if !valid {
		hint = i18n.T("同步目标已改变，请先重新同步")
	}
	ui.Text(c, hint).FontSize(12).TextColor(c.Theme().Text)
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c.Key("confirm-overwrite"), i18n.T("确认覆盖并同步")).Disabled(!valid).Clicked() {
			direction := s.confirmReconcile
			if a.saveCloudSync() {
				s.reconcile, s.resolve, s.recheck = direction, nil, false
				a.startCloudSync(false, true)
			}
		}
		if ui.Button(c.Key("cancel-overwrite"), i18n.T("取消覆盖")).Clicked() {
			s.confirmReconcile = ""
		}
	})
}

// cloudRecoveryActions offers a different operation when a plain retry cannot
// help. A recheck keeps the baseline and exposes conflicts for explicit choice.
func (a *App) cloudRecoveryActions(c *ui.Context) {
	s := &a.cloud
	remoteChanged := errors.Is(s.lastError, cloudsync.ErrRemoteChanged)
	hint := "检查服务地址、账号和同步目录，修改后测试连接，再重试同步"
	label, target := "检查连接设置", "connection"
	var status *cloudsync.HTTPError
	if remoteChanged {
		hint = "重新读取云端文件并核对内容，发现冲突后保留两份文件，再选择要采用的版本"
	} else if errors.As(s.lastError, &status) {
		switch status.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			hint = "请修改账号、密码或密钥，并确认该账号有同步目录的读写权限"
			label = "修改账号或密钥"
		case http.StatusConflict, http.StatusNotFound:
			hint = "请检查远端目录是否存在，路径中是否有同名文件，以及账号是否允许创建目录"
			label, target = "检查远端目录", "spaces"
		case http.StatusInsufficientStorage:
			hint = "请在云服务中释放空间，或更换有剩余空间的服务地址，再重试同步"
		}
	} else {
		var networkError net.Error
		if errors.Is(s.lastError, context.DeadlineExceeded) || errors.As(s.lastError, &networkError) {
			hint = "请检查网络、代理和服务地址，先测试连接，再重试同步"
		}
	}
	ui.Column(c.Key("cloud-recovery")).Padding(0, 16, 12, 16).Gap(8).Children(func() {
		if remoteChanged {
			a.cloudOverwriteChoices(c)
		}
		ui.Text(c, i18n.T(hint)).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if remoteChanged && ui.PrimaryButton(c.Key("recheck"), i18n.T("重新核对并同步")).Disabled(s.broken || !s.config.Enabled).Clicked() {
				if a.saveCloudSync() {
					s.recheck = true
					s.resolve = nil
					a.startCloudSync(false, true)
				}
			}
			if ui.Button(c.Key("settings"), i18n.T(label)).Clicked() {
				s.recoveryFocus = target
			}
		})
	})
}

func (a *App) settingsCloudSync(c *ui.Context) {
	t := c.Theme()
	s := &a.cloud
	cfg := &s.config
	locked := s.busy || s.broken
	w, _ := c.Size()
	twoCol := w >= 720

	settingsHeading(c, i18n.T("云端同步"), i18n.T("将选定空间的笔记和附件同步到自己的 WebDAV 或 S3；冲突保留两份"))

	// Overview: enable switch, primary actions, and immediate status feedback.
	settingsGroup(c, "cloud-overview", func() {
		settingRow(c, "enabled", i18n.T("启用云端同步"), a.cloudSummary(), func() {
			ui.Switch(c.Key("control"), &cfg.Enabled).Label(i18n.T("启用云端同步")).Disabled(locked)
		})
		settingsSeparator(c)
		ui.Row(c.Key("cloud-actions")).Padding(settingsRowInset(c), 16).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
			if ui.PrimaryButton(c.Key("sync"), i18n.T("立即同步")).Disabled(locked || !cfg.Enabled).Clicked() {
				if a.saveCloudSync() {
					a.startCloudSync(false, true)
				}
			}
			if s.busy && ui.Button(c.Key("cancel"), i18n.T("取消同步")).Clicked() {
				a.cancelCloudSync()
			}
			if ui.Button(c.Key("probe"), i18n.T("测试连接")).Disabled(locked).Clicked() {
				if a.saveCloudSync() {
					a.startCloudSync(true, true)
				}
			}
			if ui.Button(c.Key("save"), i18n.T("保存同步设置")).Disabled(locked).Clicked() {
				a.saveCloudSync()
			}
		})
		if s.message != "" {
			settingsSeparator(c)
			col := t.TextMuted
			bg := ui.Transparent
			if s.failed {
				col = t.Danger
				bg = t.Danger.Alpha(0.08)
			}
			ui.Row(c.Key("status-bar")).Padding(10, 16).Background(bg).Children(func() {
				ui.Text(c.Key("status"), s.message).FontSize(12).TextColor(col).LineHeight(1.45).Grow(1)
			})
			if s.failed && !s.busy {
				a.cloudRecoveryActions(c)
			}
		}
	})

	// Conflicts appear right under the overview so they are immediately actionable.
	if len(s.conflicts) > 0 {
		settingsSection(c, fmt.Sprintf(i18n.T("待处理冲突（%d）"), len(s.conflicts)))
		settingsGroup(c, "cloud-conflicts", func() {
			for i, conflict := range s.conflicts {
				if i > 0 {
					settingsSeparator(c)
				}
				note := i18n.T("本地与云端均有修改，已保留冲突副本")
				remoteLabel := i18n.T("采用云端版本")
				if !conflict.detail.RemoteExists {
					note = i18n.T("云端已删除该文件，本地仍有修改")
					remoteLabel = i18n.T("采用云端删除")
				}
				settingRow(c, fmt.Sprintf("conflict:%d", i), conflict.name+" / "+conflict.detail.Path, note, func() {
					choose := func(local bool) {
						s.resolve = map[string]map[string]cloudsync.Resolution{
							conflict.space: {conflict.detail.Path: {UseLocal: local, RemoteHash: conflict.detail.RemoteHash}},
						}
						a.startCloudSync(false, true)
					}
					if ui.Button(c.Key("local"), i18n.T("保留本地版本")).Disabled(s.busy).Clicked() {
						choose(true)
					}
					if ui.Button(c.Key("remote"), remoteLabel).Disabled(s.busy).Clicked() {
						choose(false)
					}
				})
			}
		})
	}

	// Connection settings.
	settingsSection(c, i18n.T("连接"))
	settingsGroup(c, "cloud-connection", func() {
		anchor := ui.Box(c.Key("connection-recovery-anchor")).Height(1)
		if s.recoveryFocus == "connection" {
			anchor.ScrollIntoView()
			s.recoveryFocus = ""
		}
		provider := 0
		if cfg.Provider == "s3" {
			provider = 1
		}
		settingRow(c, "provider-row", i18n.T("同步服务"), i18n.T("选择 WebDAV 或兼容 S3 的对象存储"), func() {
			if segmentedChoice(c.Key("provider"), &provider, "WebDAV", "S3").Label(i18n.T("同步服务")).Disabled(locked).Changed() {
				cfg.Provider = []string{"webdav", "s3"}[provider]
			}
		})
		settingsSeparator(c)
		if cfg.Provider == "s3" {
			cloudFieldRow(c, "endpoint", i18n.T("服务地址"), "https://s3.example.com", &cfg.S3Endpoint, false, locked)
			settingsSeparator(c)
			cloudFieldPair(c, "s3-target", twoCol, locked,
				cloudFieldSpec{"region", i18n.T("区域"), "us-east-1", &cfg.S3Region, false},
				cloudFieldSpec{"bucket", i18n.T("存储桶"), "my-notes", &cfg.S3Bucket, false},
			)
			settingsSeparator(c)
			cloudFieldRow(c, "prefix", i18n.T("S3 目录"), i18n.T("可选，例如 freefolio"), &cfg.S3Prefix, false, locked)
			settingsSeparator(c)
			cloudFieldPair(c, "s3-auth", twoCol, locked,
				cloudFieldSpec{"access", "Access Key", "", &cfg.S3AccessKey, false},
				cloudFieldSpec{"secret", "Secret Key", "", &cfg.S3SecretKey, true},
			)
			settingsSeparator(c)
			settingRow(c, "path-style", i18n.T("使用路径风格地址"), i18n.T("MinIO、R2 等兼容服务通常需要开启"), func() {
				ui.Switch(c.Key("control"), &cfg.S3PathStyle).Label(i18n.T("使用路径风格地址")).Disabled(locked)
			})
		} else {
			cloudFieldRow(c, "url", i18n.T("WebDAV 地址"), "https://dav.example.com/notes", &cfg.WebDAVURL, false, locked)
			settingsSeparator(c)
			cloudFieldPair(c, "webdav-auth", twoCol, locked,
				cloudFieldSpec{"user", i18n.T("账号"), "", &cfg.WebDAVUser, false},
				cloudFieldSpec{"password", i18n.T("密码"), "", &cfg.WebDAVPassword, true},
			)
		}
	})

	// Spaces to sync.
	settingsSection(c, i18n.T("同步空间"))
	settingsGroup(c, "cloud-spaces", func() {
		anchor := ui.Box(c.Key("spaces-recovery-anchor")).Height(1)
		if s.recoveryFocus == "spaces" {
			anchor.ScrollIntoView()
			s.recoveryFocus = ""
		}
		shown := 0
		for _, sp := range a.state.Spaces {
			if sp.IsExternal() {
				continue
			}
			if shown > 0 {
				settingsSeparator(c)
			}
			shown++
			remote, on := cfg.Spaces[sp.ID]
			ui.Column(c.Key("space:" + sp.ID)).Children(func() {
				ui.Row(c).Padding(10, 16).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(30, 30).Radius(controlRadius).Center().
						Background(spaceAccent(space.ThemeOf(sp).Colors[0], t.Dark).Alpha(0.12)).Children(func() {
						a.spaceGlyph(c, sp.Icon, sp.Symbol, sp.Image, sp.Name, 15, spaceAccent(space.ThemeOf(sp).Colors[0], t.Dark))
					})
					ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(2).Children(func() {
						ui.Text(c, sp.Name).FontSize(13).FontWeight(500).SingleLine()
						ui.Text(c, shortPath(sp.Root)).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(sp.Root)
					})
					if ui.Switch(c.Key("control"), &on).Label(sp.Name).Disabled(locked).Changed() {
						if on {
							remote = sp.Name
							cfg.Spaces[sp.ID] = remote
						} else {
							delete(cfg.Spaces, sp.ID)
						}
					}
				})
				if on {
					ui.Row(c.Key("remote-row")).Padding(0, 16, 12, 58).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, i18n.T("远端空间目录")).FontSize(12).TextColor(t.TextMuted).Shrink(0)
						if ui.TextInput(c.Key("remote"), &remote).Grow(1).MinWidth(0).Placeholder(sp.Name).
							Label(i18n.T("远端空间目录") + " · " + sp.Name).Disabled(locked).Changed() {
							cfg.Spaces[sp.ID] = remote
						}
					})
				}
			})
		}
		if shown == 0 {
			ui.Row(c).Padding(14, 16).Children(func() {
				ui.Text(c, i18n.T("暂无可同步的空间")).FontSize(13).TextColor(t.TextMuted)
			})
		}
	})

	// Automatic schedule.
	settingsSection(c, i18n.T("自动同步"))
	settingsGroup(c, "cloud-schedule", func() {
		settingRow(c, "auto", i18n.T("自动同步"), i18n.T("开启时不会立即同步；之后保存文档或到达计划时间时同步"), func() {
			ui.Switch(c.Key("control"), &cfg.Automatic).Label(i18n.T("自动同步")).Disabled(locked)
		})
		if cfg.Automatic {
			settingsSeparator(c)
			mode := 0
			if cfg.Schedule == "daily" {
				mode = 1
			}
			settingRow(c, "schedule-row", i18n.T("同步计划"), "", func() {
				if segmentedChoice(c.Key("schedule"), &mode, i18n.T("按间隔"), i18n.T("每天定时")).Label(i18n.T("同步计划")).Disabled(locked).Changed() {
					cfg.Schedule = []string{"interval", "daily"}[mode]
				}
			})
			settingsSeparator(c)
			if cfg.Schedule == "daily" {
				settingRow(c, "daily", i18n.T("每天同步时间"), i18n.T("24 小时制，例如 21:00"), func() {
					ui.TextInput(c.Key("input"), &cfg.DailyTime).Width(112).Placeholder("21:00").
						Label(i18n.T("每天同步时间")).Disabled(locked)
				})
			} else {
				cloudNumberRow(c, "interval", i18n.T("间隔（分钟）"), "", &cfg.IntervalMinutes, 1, 1440, locked)
			}
			settingsSeparator(c)
			cloudNumberRow(c, "limit", i18n.T("每日最多次数（0 表示不限）"), "", &cfg.DailyLimit, 0, 1440, locked)
			settingsSeparator(c)
			cloudNumberRow(c, "retry", i18n.T("失败重试次数"), "", &cfg.RetryLimit, 0, 10, locked)
			settingsSeparator(c)
			settingRow(c, "startup", i18n.T("启动后同步一次"), i18n.T("仅在应用启动时执行；运行期间开启自动同步不会补跑"), func() {
				ui.Switch(c.Key("control"), &cfg.SyncOnStart).Label(i18n.T("启动后同步一次")).Disabled(locked)
			})
		}
	})

	// Backups & encryption.
	settingsSection(c, i18n.T("备份与加密"))
	settingsGroup(c, "cloud-safety", func() {
		cloudNumberRow(c, "retention", i18n.T("每个文件保留的远端备份数"), i18n.T("远端覆盖或删除前保留的历史副本数量"), &cfg.KeepSnapshots, 1, 100, locked)
		settingsSeparator(c)
		settingRow(c, "backups-row", i18n.T("本地恢复备份"), i18n.T("同步替换或删除本地文件前的恢复副本"), func() {
			if ui.Button(c.Key("backups"), i18n.T("打开恢复备份")).Clicked() {
				mygo.Shell.OpenPath(filepath.Join(a.cfg.ConfigDir, "sync-state", "backups"))
			}
		})
		settingsSeparator(c)
		settingRow(c, "encrypt", i18n.T("加密云端文件内容"), i18n.T("加密时文件名仍可见；每台电脑需填写相同密码，忘记密码无法恢复内容"), func() {
			ui.Switch(c.Key("control"), &cfg.Encrypt).Label(i18n.T("加密云端文件内容")).Disabled(locked)
		})
		if cfg.Encrypt {
			settingsSeparator(c)
			cloudFieldRow(c, "encryption-password", i18n.T("内容加密密码"), i18n.T("输入加密密码"), &cfg.EncryptPassword, true, locked)
		}
	})

	ui.Text(c, i18n.T("设置和凭据仅保存在本机；其他电脑选择相同的远端目录即可同步")).
		FontSize(12).TextColor(t.TextMuted).Padding(0, 2)
}

func (a *App) cloudSummary() string {
	s := &a.cloud
	cfg := &s.config
	if s.broken {
		return i18n.T("同步设置无法读取，原文件已保留")
	}
	if !cfg.Enabled {
		return i18n.T("已关闭 · 开启后可手动或自动同步选定空间")
	}
	selected := 0
	for id := range cfg.Spaces {
		if sp := a.state.Get(id); sp != nil && !sp.IsExternal() {
			selected++
		}
	}
	if selected == 0 {
		return i18n.T("已启用 · 请在下方选择至少一个同步空间")
	}
	if !s.last.IsZero() {
		return fmt.Sprintf(i18n.T("上次同步 %s · 已选 %d 个空间"), s.last.Format("15:04:05"), selected)
	}
	return fmt.Sprintf(i18n.T("已启用 · 已选 %d 个空间"), selected)
}

// cloudFailure returns the first line of the last sync failure, or "" when
// nothing failed or a run is in progress (its progress is shown instead).
func (a *App) cloudFailure() string {
	s := &a.cloud
	if s.busy || !s.failed && !s.broken {
		return ""
	}
	if msg := strings.TrimSpace(strings.SplitN(s.message, "\n", 2)[0]); msg != "" {
		return msg
	}
	return i18n.T("同步出现问题")
}

// cloudProblem sums up what needs the user's attention: a failure and the
// conflicts left to resolve, or "" when sync is fine.
func (a *App) cloudProblem() string {
	var parts []string
	if f := a.cloudFailure(); f != "" {
		parts = append(parts, f)
	}
	if n := len(a.cloud.conflicts); n > 0 && !a.cloud.busy {
		parts = append(parts, fmt.Sprintf(i18n.T("%d 个冲突待处理"), n))
	}
	return strings.Join(parts, " · ")
}

// sidebarCloudActions renders compact cloud-sync icons in the sidebar footer:
// a direct sync/cancel button and a quick menu for toggling spaces and settings.
func (a *App) sidebarCloudActions(c *ui.Context) {
	t := c.Theme()
	s := &a.cloud
	cfg := &s.config
	locked := s.busy || s.broken

	// Direct Sync Now / Cancel Sync button.
	syncLabel := i18n.T("立即同步")
	syncTip := syncLabel
	syncIcon := iconCloudSync
	syncDisabled := !cfg.Enabled || s.broken
	if s.busy {
		syncLabel = i18n.T("取消同步")
		syncTip = syncLabel
		if s.message != "" {
			syncTip += " · " + strings.SplitN(s.message, "\n", 2)[0]
		}
		syncIcon = iconX
		syncDisabled = false
	} else if p := a.cloudProblem(); p != "" {
		syncLabel = i18n.T("解决同步问题")
		syncTip = syncLabel + " · " + p
		syncDisabled = false
	} else if syncDisabled {
		syncTip = i18n.T("立即同步（未启用云端同步）")
	} else if !s.last.IsZero() {
		syncTip = fmt.Sprintf(i18n.T("立即同步 · 上次 %s"), s.last.Format("15:04:05"))
	}
	syncBtn := iconButton(c, "sidebar-cloud-sync", syncIcon, syncLabel, syncTip, syncDisabled).Size(28, 28)
	if s.busy {
		syncBtn.TextColor(t.Success).Background(t.Success.Alpha(0.10))
	}
	if syncBtn.Clicked() {
		if s.busy {
			a.cancelCloudSync()
		} else if s.failed || len(s.conflicts) > 0 {
			a.openSettings(tabCloud)
		} else {
			a.startCloudSync(false, true)
		}
	}

	// Cloud Sync quick menu: choose sync spaces, toggle enabled, test, or open settings.
	cloudTip := i18n.T("云端同步") + " · " + a.cloudSummary()
	if s.busy && s.message != "" {
		cloudTip = i18n.T("云端同步") + " · " + strings.SplitN(s.message, "\n", 2)[0]
	} else if p := a.cloudProblem(); p != "" {
		cloudTip = i18n.T("云端同步") + " · " + p
	}
	cloudBtn := iconButton(c, "sidebar-cloud-menu", settingsIcons["cloud"], i18n.T("云端同步"), cloudTip, false).
		Size(28, 28).Role(ui.RoleMenuButton)
	// Green says sync is on whatever the space's accent is (which may be
	// red); red stays for failures and conflicts.
	switch {
	case s.failed || len(s.conflicts) > 0:
		cloudBtn.TextColor(t.Danger)
	case s.busy || cfg.Enabled:
		cloudBtn.TextColor(t.Success)
	}
	cloudBtn.PopoverMenuButton(func(m *ui.Menu) {
		m.MaxWidth(340)
		// Lead with the failure so it is seen before any action is chosen.
		if f := a.cloudFailure(); f != "" {
			if m.Item(i18n.T("同步出现问题")).Subtitle(f).Tooltip(f).Danger().Chosen() {
				a.openSettings(tabCloud)
			}
			m.Separator()
		}
		if s.busy {
			if m.Item(i18n.T("取消同步")).Chosen() {
				a.cancelCloudSync()
			}
		} else {
			if m.Item(i18n.T("立即同步")).Disabled(locked || !cfg.Enabled).Chosen() {
				a.startCloudSync(false, true)
			}
		}
		m.Submenu(i18n.T("选择同步空间"), func(sub *ui.Menu) {
			shown := 0
			for _, sp := range a.state.Spaces {
				if sp.IsExternal() {
					continue
				}
				shown++
				_, on := cfg.Spaces[sp.ID]
				if sub.Item(sp.Name).Checked(on).Disabled(locked).Chosen() {
					if on {
						delete(cfg.Spaces, sp.ID)
					} else {
						if cfg.Spaces == nil {
							cfg.Spaces = map[string]string{}
						}
						cfg.Spaces[sp.ID] = sp.Name
					}
					a.saveCloudSync()
				}
			}
			if shown == 0 {
				sub.Item(i18n.T("暂无可同步的空间")).Disabled(true)
			}
		})
		if m.Item(i18n.T("启用云端同步")).Checked(cfg.Enabled).Disabled(locked).Chosen() {
			cfg.Enabled = !cfg.Enabled
			a.saveCloudSync()
		}
		if m.Item(i18n.T("测试连接")).Disabled(locked).Chosen() {
			a.startCloudSync(true, true)
		}
		if len(s.conflicts) > 0 {
			m.Separator()
			if m.Item(fmt.Sprintf(i18n.T("待处理冲突（%d）"), len(s.conflicts))).Danger().Chosen() {
				a.openSettings(tabCloud)
			}
		}
		m.Separator()
		if m.Item(i18n.T("云端同步设置")).Chosen() {
			a.openSettings(tabCloud)
		}
	})
}

type cloudFieldSpec struct {
	key, label, placeholder string
	value                   *string
	password                bool
}

func cloudFieldRow(c *ui.Context, key, label, placeholder string, value *string, password, disabled bool) {
	ui.Column(c.Key(key)).Padding(settingsRowInset(c)+2, 16).Gap(6).Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(500)
		in := ui.TextInput(c.Key("input"), value).FillWidth().Placeholder(placeholder).Label(label).Disabled(disabled)
		if password {
			in.Password()
		}
	})
}

func cloudFieldPair(c *ui.Context, key string, twoCol, disabled bool, left, right cloudFieldSpec) {
	if !twoCol {
		cloudFieldRow(c, left.key, left.label, left.placeholder, left.value, left.password, disabled)
		settingsSeparator(c)
		cloudFieldRow(c, right.key, right.label, right.placeholder, right.value, right.password, disabled)
		return
	}
	ui.Row(c.Key(key)).Padding(settingsRowInset(c)+2, 16).Gap(12).Children(func() {
		for _, spec := range []cloudFieldSpec{left, right} {
			ui.Column(c.Key(spec.key)).Grow(1).MinWidth(0).Gap(6).Children(func() {
				ui.Text(c, spec.label).FontSize(13).FontWeight(500)
				in := ui.TextInput(c.Key("input"), spec.value).FillWidth().Placeholder(spec.placeholder).Label(spec.label).Disabled(disabled)
				if spec.password {
					in.Password()
				}
			})
		}
	})
}

func cloudNumberRow(c *ui.Context, key, label, note string, value *int, lo, hi int, disabled bool) {
	settingRow(c, key, label, note, func() {
		v := float64(*value)
		if ui.NumberInput(c.Key("number"), &v, float64(lo), float64(hi), 1).Width(112).Label(label).Disabled(disabled).Changed() {
			*value = int(v)
		}
	})
}
