package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type gitRepositoryDraft struct {
	space, name, fingerprint string
}

func (gs *gitUIState) prepareGitRepository(id string) {
	sp := gs.host.spaces().Get(id)
	if sp == nil || gs.busy {
		return
	}
	if gs.host.cloudBusy() || gs.host.cloudSyncsSpace(id) {
		gs.gitError(errors.New(i18n.T("请先关闭此空间的云端同步")))
		return
	}
	if !gs.host.flushForTask() {
		return
	}
	root, name := sp.Root, sp.Name
	if gs.creation != nil && gs.creation.space == id {
		name = gs.creation.name
	}
	if !gitsync.ValidRepositoryName(name) {
		name = "freefolio-notes"
	}
	gs.gitWork("正在准备待上传文件", func(ctx context.Context) func() {
		g, err := gs.gitEngine(ctx, id, root)
		var repo gitsync.Repository
		if err == nil {
			repo, err = g.PrepareRepository(ctx, root)
		}
		return func() {
			if err != nil {
				gs.gitError(err)
				return
			}
			gs.applyGitRepo(id, repo)
			gs.creation = &gitRepositoryDraft{space: id, name: name, fingerprint: repo.Fingerprint}
			cfg := gs.gitSpaceConfig(id)
			if cfg.Account == "" && len(gs.accounts) == 1 {
				cfg.Account = gs.accounts[0].ID
			}
			gs.message = ""
		}
	})
}

func (gs *gitUIState) createGitRepository(id string) {
	sp := gs.host.spaces().Get(id)
	if sp == nil || gs.busy || gs.creation == nil || gs.creation.space != id {
		return
	}
	if gs.host.cloudBusy() || gs.host.cloudSyncsSpace(id) {
		gs.gitError(errors.New(i18n.T("请先关闭此空间的云端同步")))
		return
	}
	if !gs.host.flushForTask() {
		return
	}
	draft, cfg := *gs.creation, *gs.gitSpaceConfig(id)
	var account gitsync.Account
	for _, candidate := range gs.accounts {
		if candidate.ID == cfg.Account {
			account = candidate
		}
	}
	if account.Login == "" {
		gs.gitError(errors.New(i18n.T("请先连接 GitHub 账号")))
		return
	}
	paths := []string{}
	for _, change := range gs.repos[id].Changes {
		if gs.checked[id][change.Path] {
			paths = append(paths, change.Path)
		}
	}
	if len(paths) == 0 {
		gs.gitError(errors.New(i18n.T("请选择提交文件")))
		return
	}
	slices.Sort(paths)
	root, dir, client, create := sp.Root, gs.host.appConfig().ConfigDir, gs.client, gs.createRemote
	message := strings.TrimSpace(gs.commitMessage)
	if message == "" {
		message = "Initial notes"
	}
	// Persist the binding before upload, so a failed push can use the normal
	// retry controls without creating another remote or enabling a schedule.
	saved := gitsync.Config{Version: 1, Spaces: map[string]*gitsync.SpaceConfig{}}
	for key, value := range gs.saved {
		copy := value
		saved.Spaces[key] = &copy
	}
	gs.gitWork("正在创建私有仓库并上传", func(ctx context.Context) func() {
		g, err := gs.gitEngine(ctx, id, root)
		var repo gitsync.Repository
		var remote gitsync.RemoteRepository
		if err == nil {
			repo, err = g.PrepareRepository(ctx, root)
		}
		if err == nil && repo.Fingerprint != draft.fingerprint {
			err = gitsync.ErrChanged
		}
		var token string
		if err == nil {
			token, err = client.Token(ctx, account.ID)
		}
		if err == nil {
			remote, err = create(ctx, account.ID, draft.name)
		}
		bound, persisted := false, false
		if err == nil {
			cfg.Binding, err = g.BindNewRepository(ctx, root, remote.URL, draft.fingerprint)
			bound = err == nil
		}
		if err == nil {
			cfg.Enabled, cfg.Automatic, cfg.OnStart, cfg.OnSave = false, false, false, false
			// An empty remote has no branch to pull yet. A retry must publish the
			// initial commit before the user opts into two-way synchronization.
			cfg.Mode = "push"
			saved.Spaces[id] = &cfg
			err = gitsync.SaveConfig(dir, saved)
			persisted = err == nil
		}
		if err == nil {
			var close func()
			g.Env, close, err = gitsync.CredentialSession(ctx, remote.Name, token)
			if err == nil {
				defer close()
			}
		}
		if err == nil {
			g.Env = append(g.Env, "GIT_AUTHOR_NAME="+account.Login, "GIT_AUTHOR_EMAIL="+account.Email, "GIT_COMMITTER_NAME="+account.Login, "GIT_COMMITTER_EMAIL="+account.Email)
			err = g.Commit(ctx, root, cfg.Binding, paths, message, draft.fingerprint)
		}
		if err == nil {
			err = g.Push(ctx, root, cfg.Binding)
		}
		if g != nil {
			current, inspectErr := g.Inspect(ctx, root)
			if inspectErr == nil {
				repo = current
			} else if err == nil {
				err = inspectErr
			}
		}
		return func() {
			if repo.Root != "" {
				gs.applyGitRepo(id, repo)
			}
			if bound {
				*gs.gitSpaceConfig(id) = cfg
				if persisted {
					gs.saved[id] = cfg
					gs.schedules[id] = &gitsync.Schedule{}
				}
			}
			if remote.URL != "" {
				gs.creation = nil
			}
			if err != nil {
				if remote.URL != "" {
					err = fmt.Errorf(i18n.T("私有仓库已创建：%s；上传未完成：%v"), remote.URL, err)
				}
				gs.gitError(err)
				return
			}
			gs.message = i18n.T("私有仓库已创建，选定文件已上传")
			gs.commitMessage = ""
		}
	})
}

func (gs *gitUIState) gitCreateRepositoryView(c *ui.Context, id string, repo gitsync.Repository, cfg *gitsync.SpaceConfig, locked bool) {
	settingsSection(c, i18n.T("创建私有仓库"))
	settingsGroup(c, "git-create", func() {
		settingRow(c, "account", i18n.T("同步账号"), i18n.T("仓库将创建在所选账号下，仅上传下方勾选的文件"), func() {
			label := i18n.T("选择 GitHub 账号")
			for _, account := range gs.accounts {
				if account.ID == cfg.Account {
					label = account.Login
				}
			}
			ui.PopoverMenuButton(c.Key("pick"), label, func(m *ui.Menu) {
				for _, account := range gs.accounts {
					if m.Item(account.Login).Checked(account.ID == cfg.Account).Chosen() {
						cfg.Account = account.ID
					}
				}
			}).Disabled(locked)
		})
		settingsSeparator(c)
		gitFieldRow(c, "name", i18n.T("仓库名称"), i18n.T("仓库名称只能包含英文字母、数字、点、短横线和下划线，最多 100 个字符"), "freefolio-notes", &gs.creation.name, locked)
	})
	gs.gitWorkspaceChangesView(c, id, repo, cfg, locked)
	selected := false
	for _, change := range repo.Changes {
		selected = selected || gs.checked[id][change.Path]
	}
	ui.Row(c.Key("git-create-actions")).Gap(8).Wrap().Children(func() {
		if ui.PrimaryButton(c.Key("confirm"), i18n.T("创建私有仓库并上传")).Disabled(locked || cfg.Account == "" || !selected || !gitsync.ValidRepositoryName(gs.creation.name)).Clicked() {
			gs.createGitRepository(id)
		}
		if ui.Button(c.Key("refresh"), i18n.T("重新检查待上传文件")).Disabled(locked).Clicked() {
			gs.prepareGitRepository(id)
		}
		if ui.Button(c.Key("cancel"), i18n.T("取消")).Disabled(locked).Clicked() {
			gs.creation = nil
		}
	})
}
