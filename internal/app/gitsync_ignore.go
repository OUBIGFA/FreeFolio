package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/gitsync"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

type gitExclusionDraft struct {
	space, filter string
	state         gitsync.ExclusionState
	selected      map[string]bool
}

func (gs *gitUIState) openGitExclusions(id string) {
	sp := gs.host.spaces().Get(id)
	if sp == nil || gs.busy {
		return
	}
	root := sp.Root
	gs.gitWork("正在读取同步排除", func(ctx context.Context) func() {
		g, err := gs.gitEngine(ctx, id, root)
		var state gitsync.ExclusionState
		if err == nil {
			if _, inspectErr := g.Inspect(ctx, root); inspectErr != nil {
				_, err = g.PrepareRepository(ctx, root)
			}
		}
		if err == nil {
			state, err = g.Exclusions(ctx, root)
		}
		return func() {
			if err != nil {
				gs.gitError(err)
				return
			}
			draft := &gitExclusionDraft{space: id, state: state, selected: map[string]bool{}}
			for _, path := range state.Paths {
				draft.selected[path] = true
			}
			gs.exclusions = draft
			gs.message = ""
		}
	})
}

func (gs *gitUIState) saveGitExclusions(id string) {
	sp := gs.host.spaces().Get(id)
	if sp == nil || gs.busy || gs.exclusions == nil || gs.exclusions.space != id {
		return
	}
	if gs.host.cloudBusy() || gs.host.cloudSyncsSpace(id) {
		gs.gitError(errors.New(i18n.T("请先关闭此空间的云端同步")))
		return
	}
	if !gs.host.flushForTask() {
		return
	}
	root, fingerprint := sp.Root, gs.exclusions.state.Fingerprint
	paths := []string{}
	for path, on := range gs.exclusions.selected {
		if on {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	gs.gitWork("正在保存同步排除", func(ctx context.Context) func() {
		g, err := gs.gitEngine(ctx, id, root)
		var repo gitsync.Repository
		if err == nil {
			err = g.SetExclusions(ctx, root, paths, fingerprint)
		}
		if err == nil {
			repo, err = g.Inspect(ctx, root)
		}
		return func() {
			if err != nil {
				gs.gitError(err)
				return
			}
			gs.applyGitRepo(id, repo)
			// The exclusion operation changes only bookkeeping. Files still need
			// the creation page's explicit selection and upload confirmation.
			if gs.creation != nil && gs.creation.space == id {
				gs.creation.fingerprint = repo.Fingerprint
			}
			gs.exclusions = nil
			gs.message = i18n.T("排除已保存，本地文件已保留，云端移除将在下次提交并推送后生效")
		}
	})
}

func (gs *gitUIState) gitExclusionsView(c *ui.Context, id string, locked bool) {
	draft := gs.exclusions
	settingsSection(c, i18n.T("同步排除"))
	settingsGroup(c, "git-exclusions", func() {
		ui.Column(c).Padding(20).Gap(12).Children(func() {
			ui.Text(c, i18n.T("勾选不想同步的文件或文件夹，保存后保留本地文件，已上传内容将在下次提交时从云端当前版本移除")).FontSize(12.5).LineHeight(1.45)
			ui.TextInput(c.Key("filter"), &draft.filter).FillWidth().Placeholder(i18n.T("筛选文件或文件夹")).Label(i18n.T("筛选文件或文件夹")).Disabled(locked)
		})
		settingsSeparator(c)
		ui.Scroll(c.Key("entries")).Height(320).Padding(8, 0).Gap(4).Children(func() {
			entries := slices.Clone(draft.state.Entries)
			// Keep removed paths available so stale exclusions can be cancelled.
			known := map[string]bool{}
			for _, entry := range entries {
				known[entry.Path] = true
			}
			for _, path := range draft.state.Paths {
				if !known[path] {
					entries = append(entries, gitsync.ExclusionEntry{Path: path, Directory: strings.HasSuffix(path, "/")})
				}
			}
			slices.SortFunc(entries, func(a, b gitsync.ExclusionEntry) int { return strings.Compare(a.Path, b.Path) })
			filter := strings.ToLower(strings.TrimSpace(draft.filter))
			for _, entry := range entries {
				path := entry.Path
				if filter != "" && !strings.Contains(strings.ToLower(path), filter) {
					continue
				}
				inherited := false
				for parent, on := range draft.selected {
					if on && parent != path && strings.HasSuffix(parent, "/") && strings.HasPrefix(path, parent) {
						inherited = true
						break
					}
				}
				on := inherited || draft.selected[path]
				ui.Row(c.Key(path)).Gap(12).Padding(8, 0).AlignItems(ui.Center).Children(func() {
					if ui.Checkbox(c.Key("exclude"), &on, "").Label(path).Disabled(locked || inherited).Changed() {
						draft.selected[path] = on
					}
					ui.Text(c, path).FontSize(12.5).Grow(1).MinWidth(0).SingleLine().Tooltip(path)
					if inherited {
						settingsBadge(c, i18n.T("随文件夹排除"), "")
					}
				})
			}
		})
	})
	ui.Row(c.Key("git-exclusion-actions")).Gap(12).Wrap().Children(func() {
		if ui.PrimaryButton(c.Key("save"), i18n.T("保存同步排除")).Disabled(locked).Clicked() {
			gs.saveGitExclusions(id)
		}
		if ui.Button(c.Key("refresh"), i18n.T("重新读取")).Disabled(locked).Clicked() {
			gs.openGitExclusions(id)
		}
		if ui.Button(c.Key("cancel"), i18n.T("取消")).Disabled(locked).Clicked() {
			gs.exclusions = nil
		}
	})
}
