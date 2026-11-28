package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (gs *gitUIState) gitConfigDirty(id string) bool {
	if !gs.loaded || id == "" {
		return false
	}
	cur := gs.config.Spaces[id]
	if cur == nil {
		return false
	}
	base := *gitsync.DefaultSpace()
	if saved, ok := gs.saved[id]; ok {
		base = saved
	}
	return *cur != base
}

func gitStatusBanner(c *ui.Context, key, text, tone string, busy bool, dismiss func(), action func()) {
	t := c.Theme()
	bg, border, ink := t.SurfaceHover, separator(t), t.Text
	switch tone {
	case "accent":
		bg, border = t.Accent.Alpha(0.08), t.Accent.Alpha(0.24)
	case "danger":
		bg, border, ink = t.Danger.Alpha(0.08), t.Danger.Alpha(0.24), t.Danger
	case "success":
		bg, border = t.Success.Alpha(0.08), t.Success.Alpha(0.24)
	}
	ui.Row(c.Key(key)).WidthPercent(100).Padding(10, 14).Gap(10).Radius(controlRadius).
		Background(bg).Border(1, border).AlignItems(ui.Center).Children(func() {
		switch {
		case busy:
			ui.Spinner(c.Key("spinner")).TextColor(t.Accent).Label(text)
		case tone == "danger":
			ui.Icon(c, iconAlert).FontSize(15).TextColor(t.Danger).Shrink(0)
		case tone == "success":
			ui.Icon(c, iconCheck).FontSize(15).TextColor(t.Success).Shrink(0)
		}
		ui.Text(c, text).FontSize(12.5).TextColor(ink).LineHeight(1.45).Grow(1).MinWidth(0)
		if action != nil {
			ui.Row(c).Shrink(0).Gap(6).AlignItems(ui.Center).Children(action)
		} else if !busy && dismiss != nil {
			closeBtn := ui.Box(c.Key("dismiss")).Size(22, 22).Radius(controlRadius).Shrink(0).Center().
				Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
				Label(i18n.T("关闭提示")).Tooltip(i18n.T("关闭提示"))
			closeInk := t.TextMuted
			if closeBtn.Hovered() {
				closeBtn.Background(hoverFill(t))
				closeInk = t.Text
			}
			closeBtn.Children(func() {
				ui.Icon(c, iconX).FontSize(12).TextColor(closeInk)
			})
			if closeBtn.Clicked() {
				dismiss()
			}
		}
	})
}

type gitFieldSpec struct {
	key, label, placeholder string
	value                   *string
	weight                  float32
}

func gitFieldRow(c *ui.Context, key, label, note, placeholder string, value *string, disabled bool) {
	t := c.Theme()
	ui.Column(c.Key(key)).Padding(settingsRowInset(c)+2, 12).Gap(6).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(500)
			if note != "" {
				ui.Text(c, note).FontSize(12).TextColor(t.TextMuted).LineHeight(1.4)
			}
		})
		ui.TextInput(c.Key("input"), value).FillWidth().Placeholder(placeholder).Label(label).Disabled(disabled)
	})
}

func gitFieldPair(c *ui.Context, key string, twoCol, disabled bool, left, right gitFieldSpec) {
	if !twoCol {
		gitFieldRow(c, left.key, left.label, "", left.placeholder, left.value, disabled)
		settingsSeparator(c)
		gitFieldRow(c, right.key, right.label, "", right.placeholder, right.value, disabled)
		return
	}
	ui.Row(c.Key(key)).Padding(settingsRowInset(c)+2, 12).Gap(12).Children(func() {
		for _, spec := range []gitFieldSpec{left, right} {
			w := spec.weight
			if w <= 0 {
				w = 1
			}
			ui.Column(c.Key(spec.key)).Grow(w).MinWidth(0).Gap(6).Children(func() {
				ui.Text(c, spec.label).FontSize(13).FontWeight(500)
				ui.TextInput(c.Key("input"), spec.value).FillWidth().Placeholder(spec.placeholder).Label(spec.label).Disabled(disabled)
			})
		}
	})
}

func gitChangeTone(code string) string {
	if strings.ContainsAny(code, "DU") {
		return "danger"
	}
	if code == "??" || strings.Contains(code, "A") {
		return "success"
	}
	return "accent"
}

func gitInitial(login string) string {
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(login))
	if r == utf8.RuneError || r == 0 {
		return "G"
	}
	return string(unicode.ToUpper(r))
}

func (gs *gitUIState) settingsGitSync(c *ui.Context) {
	gs.initGitSync()
	s := gs
	t := c.Theme()
	w, _ := c.Size()
	twoCol := w >= 720
	if s.selected == "" {
		s.selected = gs.host.spaces().Active
	}
	sp := gs.host.spaces().Get(s.selected)
	settingsHeading(c, i18n.T("GitHub 仓库同步"), i18n.T("登录授权后同步仓库空间，支持计划同步和 OneBot 提交说明"))
	if s.busy {
		msg := s.message
		if msg == "" {
			msg = i18n.T("正在执行 Git 同步")
		}
		gitStatusBanner(c, "git-status-banner", msg, "accent", true, nil, func() {
			if ui.Button(c.Key("git-cancel"), i18n.T("取消当前操作")).Clicked() {
				gs.cancelGit()
			}
		})
	} else if s.message != "" {
		tone := "success"
		if s.failed {
			tone = "danger"
		}
		gitStatusBanner(c, "git-status-banner", s.message, tone, false, func() {
			s.message = ""
			s.failed = false
		}, nil)
	}

	settingsSection(c, i18n.T("GitHub 账号"))
	settingsGroup(c, "git-account", func() {
		settingRow(c, "git-auth-row", i18n.T("授权账号"), i18n.T("在浏览器中登录并授权即可，首次连接会自动准备所需组件"), func() {
			if len(s.accounts) == 0 {
				if ui.PrimaryButton(c.Key("login"), i18n.T("连接 GitHub")).Disabled(s.busy || !s.loaded).Clicked() {
					gs.gitLogin()
				}
			} else if ui.Button(c.Key("login"), i18n.T("连接 GitHub")).Disabled(s.busy || !s.loaded).Clicked() {
				gs.gitLogin()
			}
			if ui.Button(c.Key("access"), i18n.T("管理仓库授权")).Clicked() {
				mygo.Shell.OpenExternal("https://github.com/settings/applications")
			}
		})
		for _, account := range s.accounts {
			settingsSeparator(c)
			ui.Row(c.Key(account.ID)).Padding(settingsRowInset(c), 12).Gap(10).AlignItems(ui.Center).MinHeight(48).Children(func() {
				ui.Box(c).Size(28, 28).Radius(controlRadius).Center().Background(t.Accent.Alpha(0.12)).Shrink(0).Children(func() {
					ui.Text(c, gitInitial(account.Login)).FontSize(12).FontWeight(600).TextColor(t.Accent).NoWrap()
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, account.Login).FontSize(13).FontWeight(500).SingleLine()
					sub := account.Email
					if sub == "" && account.Name != "" && account.Name != account.Login {
						sub = account.Name
					}
					if sub != "" {
						ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).SingleLine()
					}
				})
				settingsBadge(c, i18n.T("已连接"), "success")
				if ui.Button(c.Key("disconnect"), i18n.T("断开账号")).Disabled(s.busy).Clicked() {
					gs.gitDisconnect(account.ID)
				}
			})
		}
	})

	settingsSection(c, i18n.T("仓库空间"))
	repo, known := s.repos[s.selected]
	cfg := (*gitsync.SpaceConfig)(nil)
	if sp != nil {
		cfg = gs.gitSpaceConfig(sp.ID)
	}
	locked := s.busy || s.broken || !s.loaded
	settingsGroup(c, "git-space", func() {
		label := i18n.T("选择空间")
		if sp != nil {
			label = sp.Name
		}
		ui.Row(c.Key("space")).Padding(settingsRowInset(c), 12).Gap(12).AlignItems(ui.Center).MinHeight(52).Children(func() {
			if sp != nil {
				accent := spaceAccent(space.ThemeOf(sp).Colors[0], t.Dark)
				ui.Box(c).Size(30, 30).Radius(controlRadius).Center().Background(accent.Alpha(0.12)).Shrink(0).Children(func() {
					gs.host.spaceGlyph(c, sp.Icon, sp.Symbol, sp.Image, sp.Name, 15, accent)
				})
				ui.Column(c).Grow(1).MinWidth(0).Clip().Gap(2).Children(func() {
					ui.Text(c, sp.Name).FontSize(13).FontWeight(500).SingleLine()
					ui.Text(c, shortPath(sp.Root)).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(sp.Root)
				})
			} else {
				ui.Text(c, i18n.T("暂无可同步的空间")).FontSize(13).TextColor(t.TextMuted).Grow(1).MinWidth(0)
			}
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.PopoverMenuButton(c.Key("pick"), label, func(m *ui.Menu) {
					for _, item := range gs.host.spaces().Spaces {
						if item.IsExternal() {
							continue
						}
						if m.Item(item.Name).Checked(item.ID == s.selected).Chosen() {
							s.selected = item.ID
							s.commitMessage = ""
							s.preview = ""
							s.conflictPath = ""
						}
					}
				}).Disabled(s.busy)
				if sp != nil && ui.Button(c.Key("inspect"), i18n.T("检查仓库")).Disabled(s.busy || !s.loaded).Clicked() {
					gs.refreshGit(sp.ID)
				}
				if sp != nil && ui.Button(c.Key("exclusions"), i18n.T("同步排除")).Disabled(locked || gs.host.cloudSyncsSpace(sp.ID)).Clicked() {
					gs.openGitExclusions(sp.ID)
				}
			})
		})
		if sp != nil && gs.host.cloudSyncsSpace(sp.ID) {
			settingsSeparator(c)
			ui.Row(c.Key("git-cloud-overlap")).Padding(8, 12).Gap(8).Background(t.Danger.Alpha(0.06)).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).FontSize(14).TextColor(t.Danger).Shrink(0)
				ui.Text(c, i18n.T("此空间已启用云端同步，无法同时启用 Git 同步")).FontSize(12).TextColor(t.Danger).LineHeight(1.4).Grow(1).MinWidth(0)
			})
		}
		if sp != nil && (gs.creation == nil || gs.creation.space != sp.ID) && cfg.Binding.Remote == "" && (!known || repo.Head == "" && len(repo.Remotes) == 0) {
			settingsSeparator(c)
			settingRow(c, "create", i18n.T("创建私有仓库"), i18n.T("为尚未使用 Git 的空间创建仓库，先检查文件再确认上传"), func() {
				if ui.Button(c.Key("prepare"), i18n.T("创建私有仓库")).Disabled(locked || len(s.accounts) == 0 || gs.host.cloudSyncsSpace(sp.ID)).Clicked() {
					gs.prepareGitRepository(sp.ID)
				}
			})
		}
		if sp != nil && known && (gs.creation == nil || gs.creation.space != sp.ID) {
			settingsSeparator(c)
			ui.Row(c.Key("git-repo-status")).Padding(10, 12, 6, 12).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Icon(c, iconGitBranch).FontSize(14).TextColor(t.Accent).Shrink(0)
				if repo.Branch != "" {
					settingsBadge(c, repo.Branch, "accent")
				}
				ui.Text(c, fmt.Sprintf(i18n.T("待推送 %d · 待拉取 %d"), repo.Ahead, repo.Behind)).FontSize(12).TextColor(t.TextMuted)
				if len(repo.Changes) > 0 {
					settingsBadge(c, fmt.Sprintf(i18n.T("%d 项改动"), len(repo.Changes)), "")
				}
				if repo.Pending != nil {
					settingsBadge(c, fmt.Sprintf(i18n.T("%d 处冲突"), len(repo.Pending.Conflicts)), "danger")
				}
			})
			ui.Row(c.Key("git-repo-actions")).Padding(4, 12, 12, 12).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				if ui.PrimaryButton(c.Key("sync"), i18n.T("立即同步")).Disabled(locked).Clicked() {
					gs.runGit(sp.ID, "sync", false)
				}
				for _, action := range []struct{ id, label string }{{"pull", "拉取"}, {"commit", "提交"}, {"push", "推送"}} {
					if ui.Button(c.Key(action.id), i18n.T(action.label)).Disabled(locked).Clicked() {
						gs.runGit(sp.ID, action.id, false)
					}
				}
				ui.Spacer(c)
				if path, e := gitsync.GitHubRepository(cfg.Binding.URL); e == nil {
					if ui.Button(c.Key("remote"), i18n.T("在 GitHub 中查看")).Clicked() {
						mygo.Shell.OpenExternal("https://github.com/" + path)
					}
				}
			})
		} else if sp != nil && (gs.creation == nil || gs.creation.space != sp.ID) {
			settingsSeparator(c)
			ui.Row(c.Key("git-repo-hint")).Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconInfo).FontSize(14).TextColor(t.TextMuted).Shrink(0)
				hint := i18n.T("点击「检查仓库」识别本地 Git 仓库、分支与远端")
				if cfg.Binding.Remote != "" {
					hint = i18n.T("点击「检查仓库」获取分支状态与未提交改动")
				}
				ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).LineHeight(1.4).Grow(1).MinWidth(0)
			})
		}
	})
	if sp == nil {
		return
	}
	if gs.exclusions != nil && gs.exclusions.space == sp.ID {
		gs.gitExclusionsView(c, sp.ID, locked)
		return
	}
	if gs.creation != nil && gs.creation.space == sp.ID {
		gs.gitCreateRepositoryView(c, sp.ID, repo, cfg, locked)
		return
	}

	if known && repo.Pending != nil {
		gs.gitConflictsView(c, sp.ID, repo.Pending, locked, twoCol)
	}
	if known {
		gs.gitWorkspaceChangesView(c, sp.ID, repo, cfg, locked)
	}

	ui.Row(c.Key("git-config-heading")).Gap(8).AlignItems(ui.Center).Children(func() {
		settingsSection(c, i18n.T("同步配置"))
		if gs.gitConfigDirty(sp.ID) {
			settingsBadge(c, i18n.T("未保存"), "accent")
		}
	})
	settingsGroup(c, "git-binding", func() {
		settingRow(c, "enabled", i18n.T("启用此空间同步"), i18n.T("保存设置后生效，开启时不会立即同步"), func() {
			ui.Switch(c.Key("switch"), &cfg.Enabled).Label(i18n.T("启用此空间同步")).Disabled(locked)
		})
		settingsSeparator(c)
		accountName := i18n.T("选择 GitHub 账号")
		for _, account := range s.accounts {
			if account.ID == cfg.Account {
				accountName = account.Login
			}
		}
		accountNote := ""
		if len(s.accounts) == 0 {
			accountNote = i18n.T("请先连接 GitHub 账号")
		}
		settingRow(c, "account", i18n.T("同步账号"), accountNote, func() {
			ui.PopoverMenuButton(c.Key("pick"), accountName, func(m *ui.Menu) {
				for _, account := range s.accounts {
					if m.Item(account.Login).Checked(account.ID == cfg.Account).Chosen() {
						cfg.Account = account.ID
					}
				}
			}).Disabled(locked || len(s.accounts) == 0)
		})
		settingsSeparator(c)
		remoteNote := cfg.Binding.URL
		if !known && remoteNote == "" {
			remoteNote = i18n.T("检查仓库后可选择远端")
		}
		settingRow(c, "remote", i18n.T("远端仓库"), remoteNote, func() {
			label := cfg.Binding.Remote
			if label == "" {
				label = i18n.T("检查仓库后选择")
			}
			btn := ui.PopoverMenuButton(c.Key("pick"), label, func(m *ui.Menu) {
				names := []string{}
				for name := range repo.Remotes {
					names = append(names, name)
				}
				slices.Sort(names)
				for _, name := range names {
					if m.Item(name).Checked(name == cfg.Binding.Remote).Chosen() {
						cfg.Binding = gitsync.Binding{Remote: name, URL: repo.Remotes[name], Branch: repo.Branch, Root: repo.Root}
					}
				}
			}).Disabled(locked || !known)
			if !known {
				btn.Tooltip(i18n.T("请先点击「检查仓库」"))
			}
		})
		settingsSeparator(c)
		branchNote := cfg.Binding.Branch
		if branchNote == "" {
			branchNote = i18n.T("尚未绑定分支")
		}
		settingRow(c, "branch", i18n.T("当前分支"), branchNote, func() {
			if known {
				if repo.Branch != "" && cfg.Binding.Branch != "" && repo.Branch != cfg.Binding.Branch {
					settingsBadge(c, fmt.Sprintf(i18n.T("当前分支：%s"), repo.Branch), "accent")
				}
				if ui.Button(c.Key("bind"), i18n.T("绑定当前分支")).Disabled(locked).Clicked() {
					cfg.Binding.Branch = repo.Branch
					cfg.Binding.URL = repo.Remotes[cfg.Binding.Remote]
					cfg.Binding.Root = repo.Root
				}
			} else if cfg.Binding.Branch != "" {
				settingsBadge(c, cfg.Binding.Branch, "")
			}
		})
		settingsSeparator(c)
		mode := slices.Index([]string{"both", "pull", "push"}, cfg.Mode)
		settingRow(c, "mode", i18n.T("同步动作"), "", func() {
			if segmentedChoice(c.Key("pick"), &mode, i18n.T("双向同步"), i18n.T("仅拉取"), i18n.T("提交并推送")).Label(i18n.T("同步动作")).Disabled(locked).Changed() {
				cfg.Mode = []string{"both", "pull", "push"}[mode]
			}
		})
	})

	settingsSection(c, i18n.T("自动同步"))
	settingsGroup(c, "git-schedule", func() {
		settingRow(c, "automatic", i18n.T("自动同步"), i18n.T("程序运行时执行，不补跑错过的任务"), func() {
			ui.Switch(c.Key("switch"), &cfg.Automatic).Label(i18n.T("自动同步")).Disabled(locked)
		})
		if cfg.Automatic {
			settingsSeparator(c)
			mode := 0
			if cfg.Plan == "daily" {
				mode = 1
			}
			settingRow(c, "plan", i18n.T("同步计划"), "", func() {
				if segmentedChoice(c.Key("pick"), &mode, i18n.T("按间隔"), i18n.T("每天定时")).Label(i18n.T("同步计划")).Disabled(locked).Changed() {
					cfg.Plan = []string{"interval", "daily"}[mode]
				}
			})
			settingsSeparator(c)
			if cfg.Plan == "daily" {
				settingRow(c, "daily", i18n.T("每天同步时间"), i18n.T("24 小时制，例如 21:00"), func() {
					ui.TextInput(c.Key("input"), &cfg.DailyTime).Width(112).Placeholder("21:00").
						Label(i18n.T("每天同步时间")).Disabled(locked)
				})
			} else {
				cloudNumberRow(c, "interval", i18n.T("间隔（分钟）"), "", &cfg.IntervalMinutes, 1, 1440, locked)
			}
			settingsSeparator(c)
			settingRow(c, "on-start", i18n.T("启动后同步一次"), i18n.T("仅在应用启动时判断"), func() {
				ui.Switch(c.Key("switch"), &cfg.OnStart).Label(i18n.T("启动后同步一次")).Disabled(locked)
			})
			settingsSeparator(c)
			settingRow(c, "on-save", i18n.T("保存后同步"), i18n.T("默认关闭，开启后可设置保存后的等待时间"), func() {
				ui.Switch(c.Key("switch"), &cfg.OnSave).Label(i18n.T("保存后同步")).Disabled(locked)
			})
			settingsSeparator(c)
			cloudNumberRow(c, "delay", i18n.T("保存后等待（秒）"), i18n.T("0 表示立即同步，再次保存会重新计时"), &cfg.SaveDelaySeconds, 0, 3600, locked || !cfg.OnSave)
			settingsSeparator(c)
			cloudNumberRow(c, "limit", i18n.T("每日最多次数（0 表示不限）"), "", &cfg.DailyLimit, 0, 1440, locked)
			settingsSeparator(c)
			cloudNumberRow(c, "retry", i18n.T("失败重试次数"), "", &cfg.RetryLimit, 0, 10, locked)
		}
		var meta []string
		if sch := s.schedules[sp.ID]; sch != nil && !sch.Next.IsZero() {
			meta = append(meta, fmt.Sprintf(i18n.T("下次同步：%s"), sch.Next.Format("01-02 15:04:05")))
		}
		if last := s.last[sp.ID]; !last.IsZero() {
			meta = append(meta, fmt.Sprintf(i18n.T("上次操作：%s"), last.Format("01-02 15:04:05")))
		}
		if len(meta) > 0 {
			settingsSeparator(c)
			ui.Row(c.Key("schedule-meta")).Padding(10, 12).Gap(12).Background(t.Text.Alpha(0.025)).Wrap().Children(func() {
				for _, item := range meta {
					ui.Text(c, item).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}
	})

	settingsSection(c, i18n.T("提交说明"))
	settingsGroup(c, "git-messages", func() {
		mode := slices.Index([]string{"manual", "template", "ai"}, cfg.MessageMode)
		settingRow(c, "mode", i18n.T("提交说明方式"), "", func() {
			if segmentedChoice(c.Key("pick"), &mode, i18n.T("手动填写"), i18n.T("固定模板"), "OneBot").Label(i18n.T("提交说明方式")).Disabled(locked).Changed() {
				cfg.MessageMode = []string{"manual", "template", "ai"}[mode]
			}
		})
		if cfg.Automatic && cfg.MessageMode == "manual" && cfg.Mode != "pull" {
			settingsSeparator(c)
			ui.Row(c.Key("manual-auto-warn")).Padding(8, 12).Gap(8).Background(t.Danger.Alpha(0.06)).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).FontSize(14).TextColor(t.Danger).Shrink(0)
				ui.Text(c, i18n.T("自动提交需要固定模板或 OneBot 说明")).FontSize(12).TextColor(t.Danger).LineHeight(1.4).Grow(1).MinWidth(0)
			})
		}
		settingsSeparator(c)
		gitFieldRow(c, "template", i18n.T("提交模板"), i18n.T("支持 {date}（时间）与 {count}（文件数）"), "Update notes {date}", &cfg.Template, locked)
		if cfg.MessageMode == "ai" {
			settingsSeparator(c)
			gs.gitModelPicker(c, cfg, locked)
			settingsSeparator(c)
			gitFieldPair(c, "ai-fields", twoCol, locked,
				gitFieldSpec{"language", i18n.T("说明语言"), i18n.T("中文"), &cfg.Language, 1},
				gitFieldSpec{"prompt", i18n.T("说明要求"), i18n.T("简短概括本次修改"), &cfg.Prompt, 2},
			)
			settingsSeparator(c)
			settingRow(c, "fallback", i18n.T("生成失败时使用模板"), i18n.T("保留错误提示，使用固定模板继续提交"), func() {
				ui.Switch(c.Key("switch"), &cfg.TemplateOnFailure).Label(i18n.T("生成失败时使用模板")).Disabled(locked)
			})
			settingsSeparator(c)
			ui.Row(c.Key("ai-cost-note")).Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconInfo).FontSize(13).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, i18n.T("启用后，本次改动将发送给所选供应商，可能产生调用费用；大改动会限制发送长度")).FontSize(12).TextColor(t.TextMuted).LineHeight(1.45).Grow(1).MinWidth(0)
			})
		}
	})

	ui.Row(c.Key("git-save-bar")).Padding(6, 2, 12, 2).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		if ui.Button(c.Key("backups"), i18n.T("打开 Git 恢复备份")).Clicked() {
			mygo.Shell.OpenPath(filepath.Join(gs.host.appConfig().ConfigDir, "git-sync-state"))
		}
		if !known {
			if path, e := gitsync.GitHubRepository(cfg.Binding.URL); e == nil {
				if ui.Button(c.Key("remote"), i18n.T("在 GitHub 中查看")).Clicked() {
					mygo.Shell.OpenExternal("https://github.com/" + path)
				}
			}
		}
		ui.Spacer(c)
		if gs.gitConfigDirty(sp.ID) {
			settingsBadge(c, i18n.T("有未保存更改"), "accent")
		}
		if ui.PrimaryButton(c.Key("git-save"), i18n.T("保存 Git 同步设置")).Disabled(locked).Clicked() {
			gs.saveGitSync()
		}
	})
}

func (gs *gitUIState) gitWorkspaceChangesView(c *ui.Context, id string, repo gitsync.Repository, cfg *gitsync.SpaceConfig, locked bool) {
	s := gs
	t := c.Theme()
	creating := gs.creation != nil && gs.creation.space == id
	settingsSection(c, i18n.T("工作区改动"))
	settingsGroup(c, "git-workspace", func() {
		ui.Column(c.Key("git-commit-box")).Padding(12).Gap(8).Children(func() {
			ui.Text(c, i18n.T("本次提交说明")).FontSize(12.5).FontWeight(500)
			placeholder := i18n.T("留空时使用所选生成方式")
			if creating {
				placeholder = i18n.T("留空时使用初始提交说明")
			}
			ui.TextInput(c.Key("git-message"), &s.commitMessage).Multiline(true).MinHeight(64).Label(i18n.T("本次提交说明")).Placeholder(placeholder).Disabled(locked)
			ui.Row(c.Key("git-message-actions")).Gap(8).Wrap().Children(func() {
				if !creating {
					genBtn := ui.Button(c.Key("generate"), i18n.T("OneBot 生成说明")).Disabled(locked || cfg.Connection == "" || len(repo.Changes) == 0)
					if cfg.Connection == "" {
						genBtn.Tooltip(i18n.T("请选择生成提交说明的 OneBot 模型"))
					}
					if genBtn.Clicked() {
						gs.runGit(id, "generate", false)
					}
				}
				if ui.Button(c.Key("diff"), i18n.T("查看选定改动")).Disabled(locked || len(repo.Changes) == 0).Clicked() {
					gs.runGit(id, "diff", false)
				}
			})
		})
		settingsSeparator(c)
		if len(repo.Changes) == 0 {
			ui.Text(c, i18n.T("没有未提交的改动")).FontSize(12.5).TextColor(t.TextMuted).Padding(12)
		} else {
			selectedCount := 0
			for _, change := range repo.Changes {
				if s.checked[id][change.Path] {
					selectedCount++
				}
			}
			ui.Row(c.Key("git-changes-toolbar")).Padding(8, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf(i18n.T("已选 %d / %d 个文件"), selectedCount, len(repo.Changes))).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0)
				if ui.Button(c.Key("select-all"), i18n.T("全选")).Disabled(locked).Clicked() {
					if s.checked[id] == nil {
						s.checked[id] = map[string]bool{}
					}
					for _, change := range repo.Changes {
						s.checked[id][change.Path] = true
					}
				}
				if ui.Button(c.Key("select-none"), i18n.T("清空选择")).Disabled(locked).Clicked() {
					if s.checked[id] == nil {
						s.checked[id] = map[string]bool{}
					}
					for _, change := range repo.Changes {
						s.checked[id][change.Path] = false
					}
				}
			})
			settingsSeparator(c)
			renderRows := func() {
				for _, change := range repo.Changes {
					path := change.Path
					on := s.checked[id][path]
					row := ui.Row(c.Key("change:"+path)).Padding(5, 6).Radius(controlRadius).Gap(8).AlignItems(ui.Center)
					if row.Hovered() {
						row.Background(hoverFill(t))
					}
					row.Children(func() {
						if ui.Checkbox(c.Key("selected"), &on, "").Label(path).Disabled(locked).Changed() {
							if s.checked[id] == nil {
								s.checked[id] = map[string]bool{}
							}
							s.checked[id][path] = on
						}
						settingsBadge(c, change.Code, gitChangeTone(change.Code))
						if change.ExcludedRemoval {
							settingsBadge(c, i18n.T("保留本地，待从云端移除"), "")
						} else if change.Staged {
							settingsBadge(c, i18n.T("已暂存"), "")
						}
						ui.Text(c, path).FontSize(12.5).Grow(1).MinWidth(0).SingleLine().Tooltip(path)
					})
				}
			}
			if len(repo.Changes) > 6 {
				ui.Scroll(c.Key("git-changes")).MaxHeight(260).MinHeight(0).Padding(4, 8).Gap(2).Children(renderRows)
			} else {
				ui.Column(c.Key("git-changes")).Padding(4, 8).Gap(2).Children(renderRows)
			}
		}
		if s.preview != "" {
			settingsSeparator(c)
			ui.Column(c.Key("git-diff-box")).Padding(12).Gap(6).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T("选定文件的改动")).FontSize(12).FontWeight(500).TextColor(t.TextMuted).Grow(1)
					if ui.Button(c.Key("hide-diff"), i18n.T("收起预览")).Clicked() {
						s.preview = ""
					}
				})
				ui.TextInput(c.Key("git-diff"), &s.preview).Multiline(true).ReadOnly(true).Height(220).Label(i18n.T("选定文件的改动"))
			})
		}
	})
}

func (gs *gitUIState) gitModelPicker(c *ui.Context, cfg *gitsync.SpaceConfig, locked bool) {
	label := i18n.T("选择 OneBot 模型")
	if conn := gs.host.preferences().AI.Find(cfg.Connection); conn != nil && cfg.Model != "" {
		label = conn.Name + " / " + cfg.Model
	}
	available := 0
	for _, conn := range gs.host.preferences().AI.Connections {
		if conn.Protocol == agent.Media {
			continue
		}
		for _, model := range conn.Models {
			if model.Enabled && (len(model.Capabilities.Output) == 0 || slices.Contains(model.Capabilities.Output, "text")) {
				available++
			}
		}
	}
	note := ""
	if available == 0 {
		note = i18n.T("请先在「设置 → OneBot」中启用文本模型")
	}
	settingRow(c, "git-model", i18n.T("OneBot 模型"), note, func() {
		ui.PopoverMenuButton(c.Key("choose"), label, func(m *ui.Menu) {
			for _, conn := range gs.host.preferences().AI.Connections {
				if conn.Protocol == agent.Media {
					continue
				}
				for _, model := range conn.Models {
					if !model.Enabled || len(model.Capabilities.Output) > 0 && !slices.Contains(model.Capabilities.Output, "text") {
						continue
					}
					if m.Item(conn.Name + " / " + model.ID).Checked(cfg.Connection == conn.ID && cfg.Model == model.ID).Chosen() {
						cfg.Connection, cfg.Model = conn.ID, model.ID
					}
				}
			}
		}).Disabled(locked || available == 0)
	})
}

func (gs *gitUIState) gitConflictsView(c *ui.Context, id string, p *gitsync.Merge, locked bool, twoCol bool) {
	s := gs
	t := c.Theme()
	resolved := 0
	for _, conflict := range p.Conflicts {
		if conflict.Resolved {
			resolved++
		}
	}
	settingsSection(c, fmt.Sprintf(i18n.T("合并冲突（%d/%d 已解决）"), resolved, len(p.Conflicts)))
	settingsGroup(c, "git-conflicts", func() {
		ui.Text(c, i18n.T("原文件保持不变，选择冲突版本后完成合并")).FontSize(12.5).TextColor(t.TextMuted).Padding(10, 12)
		for _, conflict := range p.Conflicts {
			settingsSeparator(c)
			row := ui.Row(c.Key("conflict:"+conflict.Path)).Padding(settingsRowInset(c), 12).Gap(8).AlignItems(ui.Center)
			if conflict.Path == s.conflictPath {
				row.Background(t.Accent.Alpha(0.06))
			}
			row.Children(func() {
				ui.Text(c, conflict.Path).FontSize(13).Grow(1).MinWidth(0).SingleLine().Tooltip(conflict.Path)
				if conflict.Resolved {
					settingsBadge(c, i18n.T("已解决"), "success")
					if ui.Button(c.Key("view"), i18n.T("重新查看")).Disabled(locked).Clicked() {
						s.conflictPath = conflict.Path
						gs.runGit(id, "versions", false)
					}
				} else {
					settingsBadge(c, i18n.T("待解决"), "danger")
					if ui.Button(c.Key("view"), i18n.T("查看并解决")).Disabled(locked).Clicked() {
						s.conflictPath = conflict.Path
						gs.runGit(id, "versions", false)
					}
				}
			})
		}
		if s.conflictPath != "" {
			settingsSeparator(c)
			ui.Column(c.Key("conflict-detail")).Padding(12).Gap(8).Children(func() {
				ui.Text(c, s.conflictPath).FontSize(13.5).FontWeight(500)
				if !s.versions.Binary {
					ui.Text(c, i18n.T("共同版本")).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
					base := s.versions.Base
					ui.TextInput(c.Key("共同版本"), &base).Multiline(true).ReadOnly(true).Height(84).Label(i18n.T("共同版本"))
					if twoCol {
						ui.Row(c.Key("conflict-sides")).Gap(10).Children(func() {
							for _, v := range []struct{ label, value string }{{"本地版本", s.versions.Local}, {"远端版本", s.versions.Remote}} {
								ui.Column(c.Key("col-" + v.label)).Grow(1).MinWidth(0).Gap(4).Children(func() {
									ui.Text(c, i18n.T(v.label)).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
									text := v.value
									ui.TextInput(c.Key(v.label), &text).Multiline(true).ReadOnly(true).Height(120).Label(i18n.T(v.label))
								})
							}
						})
					} else {
						for _, v := range []struct{ label, value string }{{"本地版本", s.versions.Local}, {"远端版本", s.versions.Remote}} {
							ui.Text(c, i18n.T(v.label)).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
							text := v.value
							ui.TextInput(c.Key(v.label), &text).Multiline(true).ReadOnly(true).Height(100).Label(i18n.T(v.label))
						}
					}
					ui.Text(c, i18n.T("编辑合并内容")).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
					ui.TextInput(c.Key("merged-text"), &s.conflictEdit).Multiline(true).Height(150).Label(i18n.T("编辑合并内容")).Disabled(locked)
				} else {
					ui.Text(c, i18n.T("此文件为二进制或内容过大，请选择完整版本")).FontSize(12.5).TextColor(t.TextMuted)
				}
				ui.Row(c.Key("resolve-actions")).Gap(8).Wrap().Children(func() {
					for _, v := range []struct{ id, label string }{{"local", "采用本地版本"}, {"remote", "采用远端版本"}, {"edit", "采用编辑内容"}} {
						if v.id == "edit" && s.versions.Binary {
							continue
						}
						if ui.Button(c.Key(v.id), i18n.T(v.label)).Disabled(locked).Clicked() {
							gs.runGit(id, v.id, false)
						}
					}
				})
			})
		}
		all := resolved == len(p.Conflicts)
		settingsSeparator(c)
		ui.Row(c.Key("merge-actions")).Padding(10, 12).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c.Key("abort"), i18n.T("取消合并")).Disabled(locked).Clicked() {
				gs.runGit(id, "cancel-merge", false)
			}
			if ui.PrimaryButton(c.Key("finish"), i18n.T("完成合并")).Disabled(locked || !all).Clicked() {
				gs.runGit(id, "finish", false)
			}
		})
	})
}
