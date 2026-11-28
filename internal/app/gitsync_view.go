package app

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *App) settingsGitSync(c *ui.Context) {
	a.initGitSync()
	s := &a.gitSync
	t := c.Theme()
	if s.selected == "" {
		s.selected = a.state.Active
	}
	sp := a.state.Get(s.selected)
	settingsHeading(c, i18n.T("GitHub 仓库同步"), i18n.T("登录授权后同步仓库空间，支持计划同步和 OneBot 提交说明"))
	if s.message != "" {
		tone := ""
		switch {
		case s.failed:
			tone = "danger"
		case s.busy:
			tone = "accent"
		}
		settingsBanner(c, "git-status-banner", s.message, tone, func() {
			if s.busy {
				if ui.Button(c.Key("git-cancel"), i18n.T("取消当前操作")).Clicked() {
					a.cancelGit()
				}
			}
		})
	} else if s.busy {
		settingsBanner(c, "git-status-banner", i18n.T("正在执行 Git 同步"), "accent", func() {
			if ui.Button(c.Key("git-cancel"), i18n.T("取消当前操作")).Clicked() {
				a.cancelGit()
			}
		})
	}

	settingsSection(c, i18n.T("GitHub 账号"))
	settingsGroup(c, "git-account", func() {
		settingRow(c, "git-auth-row", i18n.T("授权账号"), i18n.T("在浏览器中登录并授权即可，首次连接会自动准备所需组件"), func() {
			if len(s.accounts) == 0 {
				if ui.PrimaryButton(c.Key("login"), i18n.T("连接 GitHub")).Disabled(s.busy || !s.loaded).Clicked() {
					a.gitLogin()
				}
			} else if ui.Button(c.Key("login"), i18n.T("连接 GitHub")).Disabled(s.busy || !s.loaded).Clicked() {
				a.gitLogin()
			}
			if ui.Button(c.Key("access"), i18n.T("管理仓库授权")).Clicked() {
				mygo.Shell.OpenExternal("https://github.com/settings/applications")
			}
		})
		for _, account := range s.accounts {
			settingsSeparator(c)
			ui.Row(c.Key(account.ID)).Padding(settingsRowInset(c), 12).Gap(10).AlignItems(ui.Center).MinHeight(44).Children(func() {
				ui.Text(c, account.Login).FontSize(13).FontWeight(500).Grow(1).MinWidth(0).SingleLine()
				settingsBadge(c, i18n.T("已连接"), "accent")
				if ui.Button(c.Key("disconnect"), i18n.T("断开账号")).Disabled(s.busy).Clicked() {
					a.gitDisconnect(account.ID)
				}
			})
		}
	})

	settingsSection(c, i18n.T("仓库空间"))
	repo, known := s.repos[s.selected]
	settingsGroup(c, "git-space", func() {
		label, note := i18n.T("选择空间"), ""
		if sp != nil {
			label, note = sp.Name, shortPath(sp.Root)
		}
		settingRow(c, "space", i18n.T("空间"), note, func() {
			ui.Button(c.Key("pick"), label).Disabled(s.busy).PopoverMenuButton(func(m *ui.Menu) {
				for _, item := range a.state.Spaces {
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
			})
			if sp != nil && ui.Button(c.Key("inspect"), i18n.T("检查仓库")).Disabled(s.busy || !s.loaded).Clicked() {
				a.refreshGit(sp.ID)
			}
		})
		if sp != nil && known {
			locked := s.busy || s.broken || !s.loaded
			settingsSeparator(c)
			ui.Row(c.Key("git-repo-status")).Padding(settingsRowInset(c), 12).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
				if repo.Branch != "" {
					settingsBadge(c, repo.Branch, "accent")
				}
				ui.Text(c, fmt.Sprintf(i18n.T("待推送 %d · 待拉取 %d"), repo.Ahead, repo.Behind)).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(100)
				if ui.PrimaryButton(c.Key("sync"), i18n.T("立即同步")).Disabled(locked).Clicked() {
					a.runGit(sp.ID, "sync", false)
				}
				for _, action := range []struct{ id, label string }{{"pull", "拉取"}, {"commit", "提交"}, {"push", "推送"}} {
					if ui.Button(c.Key(action.id), i18n.T(action.label)).Disabled(locked).Clicked() {
						a.runGit(sp.ID, action.id, false)
					}
				}
			})
		}
	})
	if sp == nil {
		return
	}
	cfg := a.gitSpaceConfig(sp.ID)
	locked := s.busy || s.broken || !s.loaded

	settingsSection(c, i18n.T("同步配置"))
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
		settingRow(c, "account", i18n.T("同步账号"), "", func() {
			ui.Button(c.Key("pick"), accountName).Disabled(locked).PopoverMenuButton(func(m *ui.Menu) {
				for _, account := range s.accounts {
					if m.Item(account.Login).Checked(account.ID == cfg.Account).Chosen() {
						cfg.Account = account.ID
					}
				}
			})
		})
		settingsSeparator(c)
		settingRow(c, "remote", i18n.T("远端仓库"), cfg.Binding.URL, func() {
			label := cfg.Binding.Remote
			if label == "" {
				label = i18n.T("检查仓库后选择")
			}
			ui.Button(c.Key("pick"), label).Disabled(locked || !known).PopoverMenuButton(func(m *ui.Menu) {
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
			})
		})
		settingsSeparator(c)
		settingRow(c, "branch", i18n.T("当前分支"), cfg.Binding.Branch, func() {
			if known && ui.Button(c.Key("bind"), i18n.T("绑定当前分支")).Disabled(locked).Clicked() {
				cfg.Binding.Branch = repo.Branch
				cfg.Binding.URL = repo.Remotes[cfg.Binding.Remote]
				cfg.Binding.Root = repo.Root
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
				cloudFieldRow(c, "daily", i18n.T("每天同步时间"), "21:00", &cfg.DailyTime, false, locked)
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
			ui.Row(c.Key("schedule-meta")).Padding(8, 12).Gap(12).Wrap().Children(func() {
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
		settingsSeparator(c)
		cloudFieldRow(c, "template", i18n.T("提交模板"), "Update notes {date}", &cfg.Template, false, locked)
		if cfg.MessageMode == "ai" {
			settingsSeparator(c)
			a.gitModelPicker(c, cfg, locked)
			settingsSeparator(c)
			cloudFieldRow(c, "language", i18n.T("说明语言"), i18n.T("中文"), &cfg.Language, false, locked)
			settingsSeparator(c)
			cloudFieldRow(c, "prompt", i18n.T("说明要求"), i18n.T("简短概括本次修改"), &cfg.Prompt, false, locked)
			settingsSeparator(c)
			settingRow(c, "fallback", i18n.T("生成失败时使用模板"), i18n.T("保留错误提示，使用固定模板继续提交"), func() {
				ui.Switch(c.Key("switch"), &cfg.TemplateOnFailure).Label(i18n.T("生成失败时使用模板")).Disabled(locked)
			})
			settingsSeparator(c)
			ui.Text(c, i18n.T("启用后，本次改动将发送给所选供应商，可能产生调用费用；大改动会限制发送长度")).FontSize(12).TextColor(t.TextMuted).Padding(8, 12)
		}
	})

	ui.Row(c.Key("git-save-bar")).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		if ui.Button(c.Key("backups"), i18n.T("打开 Git 恢复备份")).Clicked() {
			mygo.Shell.OpenPath(filepath.Join(a.cfg.ConfigDir, "git-sync-state"))
		}
		if path, e := gitsync.GitHubRepository(cfg.Binding.URL); e == nil {
			if ui.Button(c.Key("remote"), i18n.T("在 GitHub 中查看")).Clicked() {
				mygo.Shell.OpenExternal("https://github.com/" + path)
			}
		}
		ui.Spacer(c)
		if ui.PrimaryButton(c.Key("git-save"), i18n.T("保存 Git 同步设置")).Disabled(locked).Clicked() {
			a.saveGitSync()
		}
	})

	if known {
		settingsSection(c, i18n.T("工作区改动"))
		settingsGroup(c, "git-workspace", func() {
			ui.Column(c.Key("git-commit-box")).Padding(12).Gap(8).Children(func() {
				ui.TextInput(c.Key("git-message"), &s.commitMessage).Multiline(true).MinHeight(64).Label(i18n.T("本次提交说明")).Placeholder(i18n.T("留空时使用所选生成方式")).Disabled(locked)
				ui.Row(c.Key("git-message-actions")).Gap(8).Wrap().Children(func() {
					if ui.Button(c.Key("generate"), i18n.T("OneBot 生成说明")).Disabled(locked || cfg.Connection == "" || len(repo.Changes) == 0).Clicked() {
						a.runGit(sp.ID, "generate", false)
					}
					if ui.Button(c.Key("diff"), i18n.T("查看选定改动")).Disabled(locked || len(repo.Changes) == 0).Clicked() {
						a.runGit(sp.ID, "diff", false)
					}
				})
			})
			settingsSeparator(c)
			if len(repo.Changes) == 0 {
				ui.Text(c, i18n.T("没有未提交的改动")).FontSize(12.5).TextColor(t.TextMuted).Padding(12)
			} else {
				ui.Column(c.Key("git-changes")).Padding(6, 12).Gap(4).Children(func() {
					for _, change := range repo.Changes {
						path := change.Path
						on := s.checked[sp.ID][path]
						ui.Row(c.Key("change:"+path)).Padding(4, 0).Gap(8).AlignItems(ui.Center).Children(func() {
							if ui.Checkbox(c.Key("selected"), &on, path).Disabled(locked).Changed() {
								s.checked[sp.ID][path] = on
							}
							settingsBadge(c, change.Code, "")
							ui.Text(c, path).FontSize(12.5).Grow(1).MinWidth(0)
						})
					}
				})
			}
			if s.preview != "" {
				settingsSeparator(c)
				ui.Column(c.Key("git-diff-box")).Padding(12).Gap(6).Children(func() {
					ui.Text(c, i18n.T("选定文件的改动")).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
					ui.TextInput(c.Key("git-diff"), &s.preview).Multiline(true).ReadOnly(true).Height(220).Label(i18n.T("选定文件的改动"))
				})
			}
		})
		if repo.Pending != nil {
			a.gitConflictsView(c, sp.ID, repo.Pending, locked)
		}
	}
}
func (a *App) gitModelPicker(c *ui.Context, cfg *gitsync.SpaceConfig, locked bool) {
	label := i18n.T("选择 OneBot 模型")
	if conn := a.prefs.AI.Find(cfg.Connection); conn != nil {
		label = conn.Name + " / " + cfg.Model
	}
	settingRow(c, "git-model", i18n.T("OneBot 模型"), "", func() {
		ui.Button(c.Key("choose"), label).Disabled(locked).PopoverMenuButton(func(m *ui.Menu) {
			for _, conn := range a.prefs.AI.Connections {
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
		})
	})
}
func (a *App) gitConflictsView(c *ui.Context, id string, p *gitsync.Merge, locked bool) {
	s := &a.gitSync
	t := c.Theme()
	settingsSection(c, i18n.T("合并冲突"))
	settingsGroup(c, "git-conflicts", func() {
		ui.Text(c, i18n.T("原文件保持不变，选择冲突版本后完成合并")).FontSize(12.5).TextColor(t.TextMuted).Padding(10, 12)
		for _, conflict := range p.Conflicts {
			settingsSeparator(c)
			ui.Row(c.Key("conflict:"+conflict.Path)).Padding(settingsRowInset(c), 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, conflict.Path).FontSize(13).Grow(1).MinWidth(0)
				if conflict.Resolved {
					settingsBadge(c, i18n.T("已解决"), "accent")
				} else if ui.Button(c.Key("view"), i18n.T("查看并解决")).Disabled(locked).Clicked() {
					s.conflictPath = conflict.Path
					a.runGit(id, "versions", false)
				}
			})
		}
		if s.conflictPath != "" {
			settingsSeparator(c)
			ui.Column(c.Key("conflict-detail")).Padding(12).Gap(8).Children(func() {
				ui.Text(c, s.conflictPath).FontSize(13.5).FontWeight(500)
				if !s.versions.Binary {
					for _, v := range []struct{ label, value string }{{"共同版本", s.versions.Base}, {"本地版本", s.versions.Local}, {"远端版本", s.versions.Remote}} {
						ui.Text(c, i18n.T(v.label)).FontSize(12).FontWeight(500).TextColor(t.TextMuted)
						text := v.value
						ui.TextInput(c.Key(v.label), &text).Multiline(true).ReadOnly(true).Height(100).Label(i18n.T(v.label))
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
							a.runGit(id, v.id, false)
						}
					}
				})
			})
		}
		all := true
		for _, v := range p.Conflicts {
			all = all && v.Resolved
		}
		settingsSeparator(c)
		ui.Row(c.Key("merge-actions")).Padding(10, 12).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c.Key("abort"), i18n.T("取消合并")).Disabled(locked).Clicked() {
				a.runGit(id, "cancel-merge", false)
			}
			if ui.PrimaryButton(c.Key("finish"), i18n.T("完成合并")).Disabled(locked || !all).Clicked() {
				a.runGit(id, "finish", false)
			}
		})
	})
}
