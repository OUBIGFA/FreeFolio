package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	model "github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/history"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func (a *App) oneBotScopeView(c *ui.Context) {
	ui.Row(c.Key("scope")).Gap(6).Wrap().Children(func() {
		label := i18n.T("选择修改范围")
		if task := a.qa.editTask; task != nil {
			label = oneBotScopeLabel(task.Scope)
		}
		ui.MenuButton(c.Key("choose"), label, func(m *ui.Menu) {
			for _, scope := range []string{"selection", "section", "document", "documents", "new"} {
				if m.Item(oneBotScopeLabel(scope)).Chosen() {
					if a.qa.editTask != nil && a.qa.editTask.Running {
						return
					}
					if scope == "documents" {
						a.qa.editScope = scope
						a.qa.editDocumentsOpen = true
					} else {
						a.beginOneBotEdit(scope)
					}
				}
			}
		})
		before := a.prefs.AI.Direct
		ui.Checkbox(c.Key("direct"), &a.prefs.AI.Direct, i18n.T("直接修改"))
		if before != a.prefs.AI.Direct {
			a.savePrefs()
		}
	})
	ui.Row(c.Key("candidate-count")).Gap(8).Children(func() {
		ui.Text(c, i18n.T("候选数量"))
		n := a.qa.editCount
		if n < 1 {
			n = 1
		}
		value := fmt.Sprint(n)
		ui.Combobox(c.Key("count"), &value, []string{"1", "2", "3", "4", "5"}).Width(70)
		fmt.Sscan(value, &a.qa.editCount)
		if ui.Button(c.Key("three"), i18n.T("提供三个方案")).Clicked() {
			a.qa.editCount = 3
			a.prefs.AI.Direct = false
			a.savePrefs()
		}
	})
	if a.qa.editScope == "new" {
		ui.TextInput(c.Key("new-path"), &a.qa.editNewPath).Placeholder("New document.md").Label(i18n.T("新文档路径"))
	}
	if task := a.qa.editTask; task != nil && len(task.Targets) > 0 {
		paths := map[string]bool{}
		for _, target := range task.Targets {
			paths[target.Path] = true
		}
		var names []string
		for p := range paths {
			names = append(names, p)
		}
		slices.Sort(names)
		ui.Text(c, strings.Join(names, " · ")).FontSize(12).TextColor(c.Theme().TextMuted)
	}
	if a.qa.editDocumentsOpen {
		ui.TextInput(c.Key("find-doc"), &a.qa.editDocQuery).Placeholder(i18n.T("搜索文档"))
		for _, node := range a.docs {
			if !strings.Contains(strings.ToLower(node.Path), strings.ToLower(a.qa.editDocQuery)) {
				continue
			}
			checked := slices.Contains(a.qa.editPaths, node.Path)
			if ui.Checkbox(c.Key(node.Path), &checked, node.Path).Changed() {
				if checked {
					a.qa.editPaths = append(a.qa.editPaths, node.Path)
				} else {
					a.qa.editPaths = slices.DeleteFunc(a.qa.editPaths, func(p string) bool { return p == node.Path })
				}
			}
		}
		if ui.Button(c.Key("confirm-docs"), i18n.T("使用选定文档")).Clicked() {
			a.beginOneBotEdit("documents")
			a.qa.editDocumentsOpen = false
		}
	}
}

func oneBotScopeLabel(scope string) string {
	switch scope {
	case "selection":
		return i18n.T("选中文字")
	case "section":
		return i18n.T("当前章节")
	case "documents":
		return i18n.T("选定多篇文档")
	case "new":
		return i18n.T("创建新文档")
	default:
		return i18n.T("整篇文档")
	}
}
func oneBotChangeStatus(s string) string {
	switch s {
	case "saved":
		return i18n.T("已保存")
	case "applied":
		return i18n.T("已应用，等待保存")
	case "rejected":
		return i18n.T("已拒绝")
	case "reverted":
		return i18n.T("已回退")
	case "conflict":
		return i18n.T("原文已变化")
	case "stale":
		return i18n.T("方案已过期")
	default:
		return i18n.T("待审阅")
	}
}

func (a *App) oneBotChangeView(c *ui.Context, task *oneBotTask, candidate, index int, ch *oneBotChange) {
	t := c.Theme()
	ui.Column(c.Key("change")).Gap(8).Padding(9, 10).Radius(9).
		Background(t.Text.Alpha(0.02)).Border(1, t.Border.Alpha(0.55)).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, ch.Path).FontSize(12.5).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			statusInk, statusBg := t.TextMuted, t.Text.Alpha(0.06)
			switch ch.Status {
			case "saved":
				statusInk, statusBg = t.Accent, t.Accent.Alpha(0.12)
			case "conflict", "stale":
				statusInk, statusBg = t.Danger, t.Danger.Alpha(0.10)
			}
			ui.Box(c.Key("status")).Padding(1, 7).Radius(5).Background(statusBg).Shrink(0).Children(func() {
				ui.Text(c, oneBotChangeStatus(ch.Status)).FontSize(11).FontWeight(500).TextColor(statusInk).NoWrap()
			})
		})
		if ch.Explanation != "" {
			ui.Text(c, ch.Explanation).FontSize(12).TextColor(t.TextMuted).LineHeight(1.45)
		}
		if len(ch.Diff) == 0 {
			before := ""
			for _, target := range task.Targets {
				if target.ID == ch.TargetID {
					before = target.Markdown
					break
				}
			}
			after := ch.Operation.Text
			if ch.NewText != "" {
				after = ch.NewText
			}
			ch.Diff = history.Diff(strings.Split(before, "\n"), strings.Split(after, "\n"))
		}
		ui.Column(c.Key("diff-lines")).Radius(7).Border(1, t.Border.Alpha(0.45)).Background(t.Surface).Clip().Children(func() {
			for i, line := range ch.Diff {
				prefix := "  "
				ink := t.Text
				fill := ui.Color{}
				switch line.Op {
				case history.Added:
					prefix = "+ "
					ink = t.Accent
					fill = t.Accent.Alpha(0.08)
				case history.Removed:
					prefix = "− "
					ink = t.Danger
					fill = t.Danger.Alpha(0.08)
				}
				ui.Box(c.Key(i)).Background(fill).Padding(4, 8).Children(func() {
					ui.Text(c, prefix+line.Text).FontSize(12.5).LineHeight(1.45).TextColor(ink)
				})
			}
		})
		if ch.Error != "" {
			ui.Text(c, ch.Error).TextColor(t.Danger).FontSize(12)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if ch.Status == "pending" {
				if oneBotApplyButton(c, "accept", i18n.T("应用这处")).Disabled(task.Applying).Clicked() {
					if err := a.applyOneBotCandidate(task, candidate, index); err != nil {
						ch.Error = err.Error()
						task.Error = err.Error()
					}
				}
				if quietOneBotAction(c, "reject", i18n.T("拒绝这处")).Disabled(task.Applying).Clicked() {
					ch.Status = "rejected"
					a.keepOneBotTask(task)
				}
			}
			if quietOneBotAction(c, "locate", i18n.T("定位原文")).Clicked() {
				a.openAnywhere(task.SpaceID, ch.Path)
				if a.doc != nil {
					if start, end, err := a.doc.ed.ResolveEdit(ch.Operation.Target); err == nil {
						a.doc.ed.SetSelection(start, end)
						a.doc.ed.RequestFocus = true
					}
				}
			}
			if ch.Status == "applied" && quietOneBotAction(c, "save", i18n.T("重试保存")).Clicked() {
				a.retryOneBotSave(task, ch)
			}
		})
	})
}

func (a *App) keepOneBotTask(task *oneBotTask) {
	if err := a.persistOneBotTask(task); err != nil {
		task.Error = err.Error()
		return
	}
	if s := a.qa.sessions[task.SpaceID]; s != nil {
		for _, turn := range s.turns {
			if turn.edit == task {
				a.saveOneBotConversation(task.SpaceID, s)
				break
			}
		}
	}
}
func (a *App) restartOneBotTask(task *oneBotTask) {
	if task.Running {
		return
	}
	a.keepOneBotTask(task)
	a.qa.editTask = nil
	a.qa.editPaths = nil
	seen := map[string]bool{}
	for _, target := range task.Targets {
		if !seen[target.Path] {
			seen[target.Path] = true
			a.qa.editPaths = append(a.qa.editPaths, target.Path)
		}
	}
	scope := task.Scope
	if scope == "selection" {
		a.notify("%s", i18n.T("请重新选择需要修改的内容"))
		return
	}
	next, err := a.captureOneBotTask(scope)
	if err != nil {
		task.Error = err.Error()
		a.qa.editTask = task
		return
	}
	a.qa.editTask = next
	a.qa.input = task.Instruction
	a.qa.focusInput = true
}

func (a *App) retryOneBotSave(task *oneBotTask, ch *oneBotChange) {
	for i := range task.Targets {
		target := &task.Targets[i]
		if target.ID != ch.TargetID {
			continue
		}
		d := target.doc
		if d == nil || d.closed.Load() {
			ch.Error = i18n.T("请重新打开文档并检查保存状态")
			return
		}
		if err := d.save(); err != nil {
			ch.Error = err.Error()
			return
		}
		ch.Status = "saved"
		ch.Error = ""
		a.keepOneBotTask(task)
		return
	}
}

func (a *App) revertOneBotCandidate(task *oneBotTask, candidate *oneBotCandidate) {
	seen := map[string]bool{}
	for i := len(candidate.Changes) - 1; i >= 0; i-- {
		ch := candidate.Changes[i]
		if ch.Status != "saved" && ch.Status != "applied" {
			continue
		}
		if ch.Receipt == nil {
			if ch.Operation.Kind == "create_document" {
				if err := a.revertOneBotCreation(task, ch); err != nil {
					ch.Error = err.Error()
				} else {
					ch.Status = "reverted"
					ch.Error = ""
				}
			} else {
				ch.Error = i18n.T("缺少修改恢复记录")
			}
			continue
		}
		if seen[ch.Batch] {
			continue
		}
		seen[ch.Batch] = true
		var d *document
		for j := range task.Targets {
			target := &task.Targets[j]
			if target.Path == ch.Path {
				d = target.doc
				break
			}
		}
		if d == nil || d.closed.Load() {
			if a.openAnywhere(task.SpaceID, ch.Path) {
				d = a.doc
			} else {
				d = nil
			}
		}
		if d == nil {
			ch.Error = i18n.T("找不到原文档")
			continue
		}
		if d.ed.Composing() || d.saving {
			ch.Error = i18n.T("请先完成输入或等待保存")
			continue
		}
		if disk, err := readFile(d.space, d.rel); err != nil || disk != d.diskText {
			ch.Error = model.ErrEditConflict.Error()
			continue
		}
		if err := d.ed.RevertAI(*ch.Receipt); err != nil {
			for _, other := range candidate.Changes {
				if other.Batch == ch.Batch {
					other.Error = err.Error()
				}
			}
			continue
		}
		if err := d.save(); err != nil {
			ch.Error = err.Error()
			ch.Status = "applied"
			continue
		}
		for _, other := range candidate.Changes {
			if other.Batch == ch.Batch {
				other.Status = "reverted"
				other.Error = ""
			}
		}
	}
	a.keepOneBotTask(task)
}

func (a *App) revertOneBotCreation(task *oneBotTask, ch *oneBotChange) error {
	sp := a.state.Get(task.SpaceID)
	if sp == nil {
		return errors.New(i18n.T("空间已不存在"))
	}
	for _, d := range a.residentDocuments() {
		if d.space.ID == sp.ID && d.rel == ch.Path && (d.dirty() || d.saving) {
			return model.ErrEditConflict
		}
	}
	disk, err := readFile(sp, ch.Path)
	if err != nil {
		return err
	}
	if disk != ch.NewText {
		return model.ErrEditConflict
	}
	full, err := resolveFile(sp, ch.Path)
	if err != nil {
		return err
	}
	kept, err := a.bin.Add(sp.ID, sp.Name, sp.Root, ch.Path, time.Now())
	if err != nil {
		return err
	}
	if err = a.trashItem(full); err != nil {
		return err
	}
	for _, d := range a.residentDocuments() {
		if d.space.ID == sp.ID && d.rel == ch.Path {
			a.replaceReferences(d, nil)
		}
	}
	a.state.Forget(sp.ID, ch.Path)
	a.saveState()
	a.refreshTree()
	a.offerUndo(i18n.T("已移到废纸篓"), ch.Path, func() { a.restoreDeleted(kept) })
	return nil
}

func (a *App) loadOneBotHistory() {
	dir := filepath.Join(a.cfg.ConfigDir, "onebot", "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		a.notify("%s", err.Error())
		return
	}
	a.qa.editHistory = nil
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 32<<20 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var task oneBotTask
		if json.Unmarshal(data, &task) != nil || task.Version != 1 {
			continue
		}
		for _, candidate := range task.Candidates {
			for _, ch := range candidate.Changes {
				ch.Receipt = task.Receipts[ch.Batch]
			}
		}
		a.qa.editHistory = append(a.qa.editHistory, &task)
	}
	slices.SortFunc(a.qa.editHistory, func(a, b *oneBotTask) int { return b.Created.Compare(a.Created) })
}
func (a *App) oneBotHistoryView(c *ui.Context) {
	t := c.Theme()
	if len(a.qa.editHistory) == 0 {
		ui.Text(c, i18n.T("暂无编辑记录")).FontSize(12.5).TextColor(t.TextMuted).Padding(6, 0)
		return
	}
	ui.Column(c).Gap(6).Children(func() {
		for _, task := range a.qa.editHistory {
			ui.Row(c.Key(task.ID)).Gap(8).Padding(7, 9).Radius(7).AlignItems(ui.Center).
				Background(t.Surface).Border(1, t.Border.Alpha(0.55)).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, task.Instruction).FontSize(12.5).FontWeight(500).SingleLine()
					ui.Text(c, task.Created.Format("2006-01-02 15:04")).FontSize(11).TextColor(t.TextMuted)
				})
				if quietOneBotAction(c, "open", i18n.T("打开记录")).Clicked() {
					a.qa.editTask = task
					a.attachOneBotTask(task)
					a.qa.editHistoryOpen = false
					a.qa.editScope = task.Scope
					a.qa.editNewPath = task.NewPath
					task.Stopped = false
				}
				del := ui.Box(c.Key("delete")).Size(26, 26).Radius(6).Center().Shrink(0).
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
					Label(i18n.T("删除记录：") + task.Instruction).Tooltip(i18n.T("删除记录"))
				delInk := t.TextMuted.Alpha(0.65)
				if del.Hovered() || del.FocusVisible() {
					del.Background(t.Danger.Alpha(0.12))
					delInk = t.Danger
				}
				del.Children(func() { ui.Icon(c, iconTrash).FontSize(13.5).TextColor(delInk) })
				if del.Clicked() {
					a.deleteOneBotEditTask(task.ID)
				}
			})
		}
	})
}
