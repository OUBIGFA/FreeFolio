package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/trashbin"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type gitUIState struct {
	applyingSpace                                          string
	saved                                                  map[string]gitsync.SpaceConfig
	messageFingerprint                                     string
	messagePaths                                           []string
	config                                                 gitsync.Config
	loaded, loading, broken, busy, failed                  bool
	selected, message, commitMessage, preview, previewPath string
	code, authURL                                          string
	client                                                 *gitsync.GitHub
	accounts                                               []gitsync.Account
	repos                                                  map[string]gitsync.Repository
	schedules                                              map[string]*gitsync.Schedule
	checked                                                map[string]map[string]bool
	last                                                   map[string]time.Time
	cancel                                                 context.CancelFunc
	done                                                   chan func()
	progress                                               chan gitsync.RuntimeProgress
	guard                                                  chan gitGuard
	protected                                              map[*document]bool
	conflictPath, conflictEdit                             string
	versions                                               gitsync.Versions
}
type gitGuard struct {
	id    string
	paths []string
	reply chan error
}

func (a *App) initGitSync() {
	s := &a.gitSync
	if s.client != nil {
		return
	}
	s.config = gitsync.Config{Version: 1, Spaces: map[string]*gitsync.SpaceConfig{}}
	s.saved = map[string]gitsync.SpaceConfig{}
	s.client = gitsync.NewGitHub(filepath.Join(a.cfg.ConfigDir, "credentials"), "")
	s.repos = map[string]gitsync.Repository{}
	s.schedules = map[string]*gitsync.Schedule{}
	s.checked = map[string]map[string]bool{}
	s.last = map[string]time.Time{}
}
func (a *App) gitSpaceConfig(id string) *gitsync.SpaceConfig {
	a.initGitSync()
	if c := a.gitSync.config.Spaces[id]; c != nil {
		return c
	}
	c := gitsync.DefaultSpace()
	a.gitSync.config.Spaces[id] = c
	return c
}
func (a *App) gitWork(label string, work func(context.Context) func()) {
	s := &a.gitSync
	if s.busy {
		return
	}
	s.busy = true
	s.failed = false
	s.message = i18n.T(label)
	s.done = make(chan func(), 1)
	s.progress = make(chan gitsync.RuntimeProgress, 1)
	s.guard = make(chan gitGuard)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	s.cancel = cancel
	done := s.done
	win := a.win
	go func() {
		defer cancel()
		apply := work(ctx)
		done <- apply
		if win != nil {
			win.Update(func() {})
		}
	}()
}
func (a *App) loadGitSync() {
	a.initGitSync()
	s := &a.gitSync
	if s.loading || s.loaded || s.busy {
		return
	}
	s.loading = true
	dir, client := a.cfg.ConfigDir, s.client
	a.gitWork("正在读取 Git 同步设置", func(ctx context.Context) func() {
		cfg, e := gitsync.LoadConfig(dir)
		accounts, accountErr := client.Accounts()
		return func() {
			s.loaded = true
			s.loading = false
			if e != nil {
				s.broken = true
				a.gitError(e)
				return
			}
			s.config = cfg
			s.accounts = accounts
			for id, c := range cfg.Spaces {
				s.saved[id] = *c
				schedule := &gitsync.Schedule{}
				schedule.Configure(c, a.now, true)
				s.schedules[id] = schedule
			}
			s.message = ""
			if accountErr != nil {
				a.gitError(accountErr)
			}
		}
	})
}
func (a *App) gitError(err error) {
	a.gitSync.failed = err != nil
	if err != nil {
		a.gitSync.message = err.Error()
	}
}
func (a *App) saveGitSync() bool {
	s := &a.gitSync
	if s.busy || s.broken {
		return false
	}
	for id, c := range s.config.Spaces {
		if c.Enabled && a.cloud.config.Enabled && a.cloud.config.Spaces[id] != "" {
			a.gitError(errors.New(i18n.T("同一空间不能同时启用 Git 与云端同步")))
			return false
		}
	}
	if e := gitsync.SaveConfig(a.cfg.ConfigDir, s.config); e != nil {
		a.gitError(e)
		return false
	}
	for id, c := range s.config.Spaces {
		s.saved[id] = *c
		sch := &gitsync.Schedule{}
		sch.Configure(c, a.now, false)
		s.schedules[id] = sch
	}
	s.message = i18n.T("Git 同步设置已保存")
	s.failed = false
	return true
}
func (a *App) gitTick(c *ui.Context) {
	s := &a.gitSync
	if s.client == nil {
		return
	}
	if s.busy {
		select {
		case p := <-s.progress:
			if p.Total > 0 {
				s.message = fmt.Sprintf(i18n.T("正在准备 Git · %.1f / %.1f MiB"), float64(p.Received)/(1<<20), float64(p.Total)/(1<<20))
			}
		default:
		}
		select {
		case request := <-s.guard:
			var e error
			for _, rel := range request.paths {
				if d := a.residentDocument(request.id, rel); d != nil && (d.saving || d.dirty() || d.ed.Composing() || d.conflict || d.ed.ReadOnly) {
					e = errors.New(i18n.T("文档有未保存修改或处于锁定状态，请处理后重试"))
					break
				}
			}
			if e == nil {
				s.applyingSpace = request.id
				if s.protected == nil {
					s.protected = map[*document]bool{}
				}
				for _, d := range a.residentDocuments() {
					if d.space.ID == request.id {
						s.protected[d] = d.ed.ReadOnly
						d.ed.SetReadOnly(true)
					}
				}
			}
			request.reply <- e
		case apply := <-s.done:
			s.busy = false
			s.cancel = nil
			for d, locked := range s.protected {
				d.ed.SetReadOnly(locked)
			}
			s.protected = nil
			s.applyingSpace = ""
			if apply != nil {
				apply()
			}
		default:
		}
		c.After(50 * time.Millisecond)
		return
	}
	if !s.loaded || s.broken {
		return
	}
	ids := make([]string, 0, len(s.schedules))
	for id := range s.schedules {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		sch := s.schedules[id]
		if sch.Ready(c.Now()) {
			a.runGit(id, "sync", true)
			break
		}
	}
	c.After(time.Second)
}
func (a *App) gitSaved(d *document) {
	if !d.justSaved {
		return
	}
	if sch := a.gitSync.schedules[d.space.ID]; sch != nil {
		sch.Saved(a.now)
	}
}
func (a *App) cancelGit() {
	if a.gitSync.cancel != nil {
		a.gitSync.cancel()
	}
}

// Let cancellation finish its on-disk rollback before the application exits.
// Worker completion does not require another UI frame after cancellation.
func (a *App) waitGitShutdown() {
	s := &a.gitSync
	if !s.busy || s.done == nil {
		return
	}
	a.cancelGit()
	select {
	case <-s.done:
	case <-time.After(35 * time.Second):
		log.Print("Git sync shutdown timed out; recovery journal retained")
	}
}

func (a *App) gitApplyingView(c *ui.Context) {
	if a.gitSync.applyingSpace == "" {
		return
	}
	ui.Overlay(c.Key("git-applying"), func() {
		ui.Column(c.Key("surface")).Fill().Modal().Center().Background(c.Theme().Background.Alpha(0.94)).Children(func() { ui.Text(c, i18n.T("正在更新空间文件")).FontSize(16) })
	})
}
func (a *App) gitLogin() {
	a.initGitSync()
	client := a.gitSync.client
	a.gitWork("正在连接 GitHub", func(ctx context.Context) func() {
		d, e := client.Begin(ctx)
		return func() {
			if e != nil {
				a.gitError(e)
				return
			}
			a.gitSync.code, a.gitSync.authURL = d.Code, d.URL
			mygo.Shell.OpenExternal(d.URL)
			a.gitWork("请在浏览器中完成 GitHub 授权", func(ctx context.Context) func() {
				account, e := client.Complete(ctx, d)
				accounts, accountErr := client.Accounts()
				if e == nil {
					e = accountErr
				}
				return func() {
					a.gitSync.code = ""
					if e != nil {
						a.gitError(e)
						return
					}
					a.gitSync.accounts = accounts
					if a.gitSync.selected != "" {
						a.gitSpaceConfig(a.gitSync.selected).Account = account.ID
					}
					a.gitSync.message = i18n.T("GitHub 已连接")
				}
			})
		}
	})
}
func (a *App) gitDisconnect(id string) {
	client := a.gitSync.client
	a.gitWork("正在断开 GitHub", func(ctx context.Context) func() {
		e := client.Disconnect(id)
		accounts, accountErr := client.Accounts()
		if e == nil {
			e = accountErr
		}
		return func() {
			a.gitSync.accounts = accounts
			a.gitError(e)
			if e == nil {
				a.gitSync.message = i18n.T("GitHub 已断开")
			}
		}
	})
}
func (a *App) gitEngine(ctx context.Context, id, root string) (*gitsync.Git, error) {
	resources := filepath.Join(a.cfg.ConfigDir, "resources")
	progress := a.gitSync.progress
	path, e := gitsync.EnsureGit(ctx, resources, func(p gitsync.RuntimeProgress) {
		select {
		case progress <- p:
		default:
		}
	})
	if e != nil {
		return nil, e
	}
	dir := a.cfg.ConfigDir
	name := id
	guard := a.gitSync.guard
	return &gitsync.Git{Path: path, StateDir: filepath.Join(dir, "git-sync-state"), Progress: func(p gitsync.RuntimeProgress) {
		select {
		case progress <- p:
		default:
		}
	}, Trash: func(path string) error {
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		_, e = (trashbin.Bin{Dir: filepath.Join(dir, "trash")}).Add(id, name, root, filepath.ToSlash(rel), time.Now())
		if e != nil {
			return e
		}
		return mygo.Shell.TrashItem(path)
	}, Guard: func(paths []string) error {
		reply := make(chan error, 1)
		select {
		case guard <- gitGuard{id, paths, reply}:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case e := <-reply:
			return e
		case <-ctx.Done():
			return ctx.Err()
		}
	}}, nil
}
func (a *App) refreshGit(id string) {
	sp := a.state.Get(id)
	if sp == nil {
		return
	}
	root := sp.Root
	a.gitWork("正在检查仓库", func(ctx context.Context) func() {
		g, e := a.gitEngine(ctx, id, root)
		var repo gitsync.Repository
		if e == nil {
			repo, e = g.Inspect(ctx, root)
		}
		return func() {
			if e != nil {
				a.gitError(e)
				return
			}
			a.applyGitRepo(id, repo)
			c := a.gitSpaceConfig(id)
			if c.Binding.Remote == "" {
				remote := "origin"
				if repo.Remotes[remote] == "" {
					for name := range repo.Remotes {
						remote = name
						break
					}
				}
				c.Binding = gitsync.Binding{Remote: remote, URL: repo.Remotes[remote], Branch: repo.Branch, Root: repo.Root}
			}
			a.gitSync.message = i18n.T("仓库检查完成")
		}
	})
}
func (a *App) applyGitRepo(id string, repo gitsync.Repository) {
	a.gitSync.repos[id] = repo
	selected := map[string]bool{}
	for _, change := range repo.Changes {
		on, exists := a.gitSync.checked[id][change.Path]
		if !exists {
			on = gitAutoPath(change.Path)
		}
		selected[change.Path] = on
	}
	a.gitSync.checked[id] = selected
}
func gitAutoPath(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == "credentials" || part == "_temp" || part == ".local" || part == ".freefolio" || part == ".arcmd" || part == "node_modules" || part == ".env" || strings.HasPrefix(part, ".env.") {
			return false
		}
	}
	return true
}

func (a *App) gitProvider(c gitsync.SpaceConfig) (agent.Provider, error) {
	conn := a.prefs.AI.Find(c.Connection)
	if conn == nil || conn.Protocol == agent.Media {
		return nil, errors.New(i18n.T("请选择可用的 OneBot 文本模型"))
	}
	settings := agent.Settings{ConnectionID: conn.ID, Protocol: conn.Protocol, BaseURL: conn.BaseURL, Vendor: conn.Vendor, Model: c.Model, Parameters: conn.Parameters, KeyStrategy: conn.KeyStrategy}
	found := false
	for _, m := range conn.Models {
		if m.ID == c.Model && m.Enabled {
			settings.Capabilities = m.Capabilities
			found = true
		}
	}
	if !found {
		return nil, errors.New(i18n.T("OneBot 模型已停用或移除"))
	}
	if conn.Protocol == agent.ChatGPT {
		a.ensureChatGPT()
		settings.TokenSource = a.chatgpt.client.Source(conn.ID)
	} else {
		keys, e := a.readAIKeys()
		if e != nil {
			return nil, e
		}
		settings.APIKeys = slices.Clone(keys.Keys[conn.ID])
		if len(settings.APIKeys) > 0 {
			settings.APIKey = settings.APIKeys[0]
		}
	}
	return settings.NewProvider(), nil
}
func (a *App) runGit(id, action string, automatic bool) {
	s := &a.gitSync
	if s.busy || s.broken {
		return
	}
	sp := a.state.Get(id)
	if sp == nil {
		return
	}
	cfg := *a.gitSpaceConfig(id)
	if automatic {
		var ok bool
		cfg, ok = s.saved[id]
		if !ok {
			return
		}
	}
	root := sp.Root
	if a.cloud.busy || a.cloud.config.Enabled && a.cloud.config.Spaces[id] != "" {
		a.gitError(errors.New(i18n.T("请先关闭此空间的云端同步")))
		if sch := s.schedules[id]; sch != nil {
			sch.Finished(a.now, false, false)
		}
		return
	}
	if action != "diff" && action != "generate" && !a.flushForTask() {
		return
	}
	var provider agent.Provider
	var providerErr error
	if action == "generate" || cfg.MessageMode == "ai" && (action == "commit" || action == "sync" && cfg.Mode != "pull") {
		provider, providerErr = a.gitProvider(cfg)
	}
	paths := []string{}
	for path, on := range s.checked[id] {
		if on {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	manualMessage := s.commitMessage
	messageFingerprint, messagePaths := s.messageFingerprint, slices.Clone(s.messagePaths)
	conflictPath, choiceText := s.conflictPath, s.conflictEdit
	account := gitsync.Account{}
	for _, v := range s.accounts {
		if v.ID == cfg.Account {
			account = v
		}
	}
	client := s.client
	started := a.now
	a.gitWork("正在处理 Git 仓库", func(ctx context.Context) func() {
		g, e := a.gitEngine(ctx, id, root)
		var repo gitsync.Repository
		message, preview := "", ""
		var versions gitsync.Versions
		fallback := ""
		if e == nil {
			repo, e = g.Inspect(ctx, root)
		}
		initialFingerprint := repo.Fingerprint
		if e == nil && !automatic && manualMessage != "" && messageFingerprint != "" && (action == "commit" || action == "sync" && cfg.Mode != "pull") && (messageFingerprint != repo.Fingerprint || !slices.Equal(paths, messagePaths)) {
			e = errors.New(i18n.T("生成说明后改动已变化，请重新生成或清空说明"))
		}
		if e == nil && automatic {
			paths = nil
			for _, change := range repo.Changes {
				if change.Staged {
					e = errors.New(i18n.T("仓库有已暂存的改动，自动同步已暂停"))
					break
				}
				if gitAutoPath(change.Path) {
					paths = append(paths, change.Path)
				}
			}
		}
		needsNetwork := action == "sync" || action == "push" || action == "pull"
		if e == nil && needsNetwork && strings.HasPrefix(cfg.Binding.URL, "https://") {
			var remote, token string
			remote, e = gitsync.GitHubRepository(cfg.Binding.URL)
			if e == nil {
				token, e = client.Token(ctx, cfg.Account)
			}
			if e == nil {
				var close func()
				g.Env, close, e = gitsync.CredentialSession(ctx, remote, token)
				if e == nil {
					defer close()
				}
			}
		}
		if e == nil && account.Login != "" {
			g.Env = append(g.Env, "GIT_AUTHOR_NAME="+account.Login, "GIT_AUTHOR_EMAIL="+account.Email, "GIT_COMMITTER_NAME="+account.Login, "GIT_COMMITTER_EMAIL="+account.Email)
		}
		if e == nil {
			switch action {
			case "diff":
				preview, e = g.Diff(ctx, root, paths)
			case "generate":
				e = providerErr
				if e == nil {
					preview, e = g.Diff(ctx, root, paths)
				}
				if e == nil {
					message, e = gitsync.GenerateMessage(ctx, provider, preview, cfg.Language, cfg.Prompt)
				}
			case "versions":
				versions, e = g.ConflictVersions(ctx, root, conflictPath)
			case "local", "remote", "edit":
				e = g.Resolve(ctx, root, conflictPath, action, choiceText)
			case "finish":
				e = g.FinishMerge(ctx, root, cfg.Binding)
			case "cancel-merge":
				e = g.CancelMerge(ctx, root)
			default:
				commit := action == "commit" || action == "sync" && cfg.Mode != "pull"
				if commit && len(paths) > 0 {
					message = manualMessage
					if automatic || message == "" {
						switch cfg.MessageMode {
						case "template":
							message = gitsync.Template(cfg, started, len(paths))
						case "ai":
							e = providerErr
							if e == nil {
								preview, e = g.Diff(ctx, root, paths)
							}
							if e == nil {
								message, e = gitsync.GenerateMessage(ctx, provider, preview, cfg.Language, cfg.Prompt)
							}
							if e != nil && cfg.TemplateOnFailure {
								fallback = e.Error()
								e = nil
								message = gitsync.Template(cfg, started, len(paths))
							}
						default:
							e = errors.New(i18n.T("请填写提交说明"))
						}
					}
					if e == nil {
						e = g.Commit(ctx, root, cfg.Binding, paths, message, repo.Fingerprint)
					}
				}
				if e == nil && (action == "pull" || action == "sync" && cfg.Mode != "push") {
					var merge *gitsync.Merge
					merge, e = g.Pull(ctx, root, cfg.Binding)
					if e == nil && merge != nil {
						e = gitsync.ErrConflict
					}
				}
				if e == nil && (action == "push" || action == "sync" && cfg.Mode != "pull") {
					e = g.Push(ctx, root, cfg.Binding)
				}
			}
		}
		if g != nil {
			if updated, inspectErr := g.Inspect(ctx, root); inspectErr == nil {
				if e == nil && action == "generate" && updated.Fingerprint != initialFingerprint {
					e = gitsync.ErrChanged
				}
				repo = updated
			} else if e == nil {
				e = inspectErr
			}
		}
		return func() {
			if repo.Root != "" {
				a.applyGitRepo(id, repo)
			}
			s.last[id] = a.now
			if sch := s.schedules[id]; sch != nil && automatic {
				retry := gitRetryable(e)
				sch.Finished(a.now, e == nil, retry)
			}
			if e != nil {
				a.gitError(e)
			} else {
				s.message = i18n.T("Git 操作已完成")
				s.failed = false
				if action == "diff" {
					s.preview = preview
				}
				if action == "generate" {
					s.commitMessage = message
					s.messageFingerprint = repo.Fingerprint
					s.messagePaths = slices.Clone(paths)
				}
				if action == "versions" {
					s.versions = versions
					s.conflictEdit = versions.Local
				}
				if fallback != "" {
					s.message = fmt.Sprintf(i18n.T("已使用提交模板，OneBot 生成失败：%s"), fallback)
				}
			}
			if e == nil && (action == "commit" || action == "sync") {
				s.commitMessage = ""
				s.messageFingerprint = ""
				s.messagePaths = nil
			}
			if action == "pull" || action == "sync" || action == "finish" {
				a.refreshTree()
				for _, d := range a.residentDocuments() {
					if d.space.ID == id && !d.dirty() && !d.saving {
						a.withDocument(d, func() { a.reload() })
					}
				}
			}
		}
	})
}

func gitRetryable(e error) bool {
	if e == nil || errors.Is(e, gitsync.ErrChanged) || errors.Is(e, gitsync.ErrConflict) || errors.Is(e, context.Canceled) {
		return false
	}
	text := strings.ToLower(e.Error())
	for _, word := range []string{"无法连接", "could not resolve host", "connection", "network", "timeout", "timed out", "rpc failed"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return errors.Is(e, context.DeadlineExceeded)
}
