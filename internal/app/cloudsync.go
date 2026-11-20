package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/cloudsync"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/space"
	"github.com/OUBIGFA/FreeMD/internal/trashbin"
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
}
type cloudConflict struct {
	space, name string
	detail      cloudsync.Conflict
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
}

func (a *App) loadCloudSync() {
	c, err := cloudsync.LoadConfig(a.cfg.ConfigDir)
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
	if s.busy || s.broken {
		return
	}
	if !probe && (!s.config.Enabled || !a.flush()) {
		return
	}
	c := s.config
	c.Spaces = maps.Clone(c.Spaces)
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
	s.message = i18n.T("正在同步…")
	if probe {
		s.message = i18n.T("正在测试连接…")
	}
	s.done = make(chan cloudOutcome, 1)
	s.guards = make(chan cloudGuard)
	done, guards := s.done, s.guards
	resolutions := s.resolve
	s.resolve = nil
	stateDir := filepath.Join(a.cfg.ConfigDir, "sync-state")
	bin := trashbin.Bin{Dir: a.bin.Dir}
	trash := a.trashFile
	if trash == nil {
		trash = mygo.Shell.TrashItem
	}
	if !probe && !manual {
		s.count++
	}
	go func() {
		defer cancel()
		out := cloudOutcome{probe: probe}
		if probe {
			out.err = cloudsync.TestConnection(ctx, c)
			done <- out
			return
		}
		for _, sp := range spaces {
			hooks := cloudsync.Hooks{
				Resolve: resolutions[sp.ID],
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
	if s.busy {
		select {
		case g := <-s.guards:
			var err error
			if d := a.residentDocument(g.space, g.rel); d != nil && (d.saving || d.dirty() || d.ed.Composing() || d.conflict || d.ed.ReadOnly) {
				err = errors.New("document has active or protected edits")
			}
			g.reply <- err
		case out := <-s.done:
			if !out.probe {
				s.conflicts = out.conflicts
			}
			s.busy = false
			s.cancel = nil
			s.last = now
			s.next = s.config.Next(now)
			s.due = time.Time{}
			s.failed = out.err != nil
			if out.err != nil {
				s.message = fmt.Sprintf(i18n.T("同步失败：%v"), out.err)
				if s.config.Automatic && s.retries < s.config.RetryLimit {
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
				if len(r.Conflicts) > 0 {
					s.message += "\n" + i18n.T("已保留冲突副本：") + strings.Join(r.Conflicts, "、")
				}
				if len(r.Skipped) > 0 {
					s.message += "\n" + i18n.T("已跳过正在编辑或只读的文件：") + strings.Join(r.Skipped, "、")
				}
			}
			if !out.probe {
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
	if !s.started {
		s.started = true
		s.next = s.config.Next(now)
		if s.config.SyncOnStart {
			s.due = now.Add(2 * time.Second)
		}
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

func (a *App) settingsCloudSync(c *ui.Context) {
	s := &a.cloud
	cfg := &s.config
	settingsHeading(c, i18n.T("云端同步"), i18n.T("将选定空间的笔记和附件同步到自己的 WebDAV 或 S3；冲突保留两份"))
	ui.Text(c, i18n.T("设置和凭据仅保存在本机；其他电脑选择相同的远端目录即可同步")).FontSize(13).TextColor(c.Theme().TextMuted)
	ui.Row(c.Key("cloud-actions")).Gap(8).Wrap().Children(func() {
		if ui.Button(c.Key("save"), i18n.T("保存同步设置")).Disabled(s.busy || s.broken).Clicked() {
			a.saveCloudSync()
		}
		if ui.Button(c.Key("probe"), i18n.T("测试连接")).Disabled(s.busy || s.broken).Clicked() {
			if a.saveCloudSync() {
				a.startCloudSync(true, true)
			}
		}
		if ui.PrimaryButton(c.Key("sync"), i18n.T("立即同步")).Disabled(s.busy || !cfg.Enabled || s.broken).Clicked() {
			if a.saveCloudSync() {
				a.startCloudSync(false, true)
			}
		}
		if s.busy && ui.Button(c.Key("cancel"), i18n.T("取消同步")).Clicked() {
			a.cancelCloudSync()
		}
		if ui.Button(c.Key("backups"), i18n.T("打开恢复备份")).Clicked() {
			mygo.Shell.OpenPath(filepath.Join(a.cfg.ConfigDir, "sync-state", "backups"))
		}
	})
	ui.Column(c.Key("cloud-form")).Gap(12).Disabled(s.busy || s.broken).Children(func() {
		cloudToggle(c, "enabled", i18n.T("启用云端同步"), &cfg.Enabled)
		provider := 0
		if cfg.Provider == "s3" {
			provider = 1
		}
		if ui.Segmented(c.Key("provider"), &provider, "WebDAV", "S3").Label(i18n.T("同步服务")).Changed() {
			cfg.Provider = []string{"webdav", "s3"}[provider]
		}
		field := func(key, label string, value *string, password bool) {
			ui.Field(c.Key(key), i18n.T(label), func() {
				in := ui.TextInput(c.Key("input"), value).FillWidth().Label(i18n.T(label))
				if password {
					in.Password()
				}
			})
		}
		if cfg.Provider == "s3" {
			field("endpoint", "服务地址", &cfg.S3Endpoint, false)
			field("region", "区域", &cfg.S3Region, false)
			field("bucket", "存储桶", &cfg.S3Bucket, false)
			field("prefix", "S3 目录", &cfg.S3Prefix, false)
			field("access", "Access Key", &cfg.S3AccessKey, false)
			field("secret", "Secret Key", &cfg.S3SecretKey, true)
			cloudToggle(c, "path-style", i18n.T("使用路径风格地址"), &cfg.S3PathStyle)
		} else {
			field("url", "WebDAV 地址", &cfg.WebDAVURL, false)
			field("user", "账号", &cfg.WebDAVUser, false)
			field("password", "密码", &cfg.WebDAVPassword, true)
		}
		settingsSeparator(c)
		ui.Text(c, i18n.T("同步空间")).FontSize(14).FontWeight(500)
		for _, sp := range a.state.Spaces {
			if sp.IsExternal() {
				continue
			}
			ui.Row(c.Key("space:" + sp.ID)).Gap(12).AlignItems(ui.Center).Children(func() {
				remote, on := cfg.Spaces[sp.ID]
				if cloudToggle(c, "switch", sp.Name, &on) {
					if on {
						remote = sp.Name
						cfg.Spaces[sp.ID] = remote
					} else {
						delete(cfg.Spaces, sp.ID)
					}
				}
				if on {
					if ui.TextInput(c.Key("remote"), &remote).Grow(1).MinWidth(0).Label(i18n.T("远端空间目录") + " · " + sp.Name).Changed() {
						cfg.Spaces[sp.ID] = remote
					}
				}
			})
		}
		settingsSeparator(c)
		cloudToggle(c, "auto", i18n.T("自动同步"), &cfg.Automatic)
		if cfg.Automatic {
			mode := 0
			if cfg.Schedule == "daily" {
				mode = 1
			}
			if ui.Segmented(c.Key("schedule"), &mode, i18n.T("按间隔"), i18n.T("每天定时")).Label(i18n.T("同步计划")).Changed() {
				cfg.Schedule = []string{"interval", "daily"}[mode]
			}
			if cfg.Schedule == "daily" {
				field("daily", "每天同步时间", &cfg.DailyTime, false)
			} else {
				cloudNumber(c, "interval", i18n.T("间隔（分钟）"), &cfg.IntervalMinutes, 1, 1440)
			}
			cloudNumber(c, "limit", i18n.T("每日最多次数（0 表示不限）"), &cfg.DailyLimit, 0, 1440)
			cloudNumber(c, "retry", i18n.T("失败重试次数"), &cfg.RetryLimit, 0, 10)
			cloudToggle(c, "startup", i18n.T("启动后同步一次"), &cfg.SyncOnStart)
		}
		cloudNumber(c, "retention", i18n.T("每个文件保留的远端备份数"), &cfg.KeepSnapshots, 1, 100)
		cloudToggle(c, "encrypt", i18n.T("加密云端文件内容"), &cfg.Encrypt)
		field("encryption-password", "内容加密密码", &cfg.EncryptPassword, true)
		ui.Text(c, i18n.T("加密时文件名仍可见；每台电脑需填写相同密码，忘记密码无法恢复内容")).FontSize(13).TextColor(c.Theme().TextMuted)
	})

	if s.message != "" {
		col := c.Theme().TextMuted
		if s.failed {
			col = c.Theme().Danger
		}
		ui.Text(c.Key("status"), s.message).FontSize(13).TextColor(col)
	}
	for i, conflict := range s.conflicts {
		ui.Column(c.Key(fmt.Sprintf("conflict:%d", i))).Gap(8).Children(func() {
			ui.Text(c, conflict.name+" / "+conflict.detail.Path).FontSize(13)
			ui.Row(c).Gap(8).Children(func() {
				choose := func(local bool) {
					s.resolve = map[string]map[string]cloudsync.Resolution{conflict.space: {conflict.detail.Path: {UseLocal: local, RemoteHash: conflict.detail.RemoteHash}}}
					a.startCloudSync(false, true)
				}
				if ui.Button(c.Key("local"), i18n.T("保留本地版本")).Disabled(s.busy).Clicked() {
					choose(true)
				}
				label := i18n.T("采用云端版本")
				if !conflict.detail.RemoteExists {
					label = i18n.T("采用云端删除")
				}
				if ui.Button(c.Key("remote"), label).Disabled(s.busy).Clicked() {
					choose(false)
				}
			})
		})
	}
}

func cloudNumber(c *ui.Context, key, label string, value *int, lo, hi int) {
	ui.Field(c.Key(key), label, func() {
		v := float64(*value)
		if ui.NumberInput(c.Key("number"), &v, float64(lo), float64(hi), 1).Label(label).Changed() {
			*value = int(v)
		}
	})
}

func cloudToggle(c *ui.Context, key, label string, value *bool) bool {
	changed := false
	ui.Row(c.Key(key)).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(13).Grow(1)
		changed = ui.Switch(c.Key("control"), value).Label(label).Changed()
	})
	return changed
}
