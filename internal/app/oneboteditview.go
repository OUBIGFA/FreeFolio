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

	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

func (ob *qaState) oneBotScopeView(c *ui.Context) {
	ui.Row(c.Key("scope")).Gap(8).Wrap().Children(func() {
		label := i18n.T("选择修改范围")
		if task := ob.editTask; task != nil {
			label = oneBotScopeLabel(task.Scope)
		}
		ui.MenuButton(c.Key("choose"), label, func(m *ui.Menu) {
			for _, scope := range []string{"selection", "section", "document", "documents", "new"} {
				if m.Item(oneBotScopeLabel(scope)).Chosen() {
					if ob.editTask != nil && ob.editTask.Running {
						return
					}
					if scope == "documents" {
						ob.editScope = scope
						ob.editDocumentsOpen = true
					} else {
						ob.beginOneBotEdit(scope)
					}
				}
			}
		})
		before := ob.host.preferences().AI.Direct
		ui.Checkbox(c.Key("direct"), &ob.host.preferences().AI.Direct, i18n.T("直接修改"))
		if before != ob.host.preferences().AI.Direct {
			ob.host.savePrefs()
		}
	})
	ui.Row(c.Key("candidate-count")).Gap(12).Children(func() {
		ui.Text(c, i18n.T("候选数量"))
		n := ob.editCount
		if n < 1 {
			n = 1
		}
		value := fmt.Sprint(n)
		ui.Combobox(c.Key("count"), &value, []string{"1", "2", "3", "4", "5"}).Width(70)
		fmt.Sscan(value, &ob.editCount)
		if ui.Button(c.Key("three"), i18n.T("提供三个方案")).Clicked() {
			ob.editCount = 3
			ob.host.preferences().AI.Direct = false
			ob.host.savePrefs()
		}
	})
	if ob.editScope == "new" {
		ui.TextInput(c.Key("new-path"), &ob.editNewPath).Placeholder("New document.md").Label(i18n.T("新文档路径"))
	}
	if task := ob.editTask; task != nil && len(task.Targets) > 0 {
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
	if ob.editDocumentsOpen {
		ui.TextInput(c.Key("find-doc"), &ob.editDocQuery).Placeholder(i18n.T("搜索文档"))
		for _, node := range ob.host.docNodes() {
			if !strings.Contains(strings.ToLower(node.Path), strings.ToLower(ob.editDocQuery)) {
				continue
			}
			checked := slices.Contains(ob.editPaths, node.Path)
			if ui.Checkbox(c.Key(node.Path), &checked, node.Path).Changed() {
				if checked {
					ob.editPaths = append(ob.editPaths, node.Path)
				} else {
					ob.editPaths = slices.DeleteFunc(ob.editPaths, func(p string) bool { return p == node.Path })
				}
			}
		}
		if ui.Button(c.Key("confirm-docs"), i18n.T("使用选定文档")).Clicked() {
			ob.beginOneBotEdit("documents")
			ob.editDocumentsOpen = false
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

func (ob *qaState) oneBotChangeView(c *ui.Context, task *oneBotTask, candidate, index int, ch *oneBotChange) {
	t := c.Theme()
	ui.Column(c.Key("change")).Gap(12).Padding(12, 16).Radius(9).
		Background(t.Text.Alpha(0.02)).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, ch.Path).FontSize(12.5).FontWeight(600).SingleLine().Grow(1).MinWidth(0)
			statusInk, statusBg := t.TextMuted, t.Text.Alpha(0.06)
			switch ch.Status {
			case "saved":
				statusInk, statusBg = t.Accent, t.Accent.Alpha(0.12)
			case "conflict", "stale":
				statusInk, statusBg = t.Danger, t.Danger.Alpha(0.10)
			}
			ui.Box(c.Key("status")).Padding(4, 12).Radius(8).Background(statusBg).Shrink(0).Children(func() {
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
		ui.Column(c.Key("diff-lines")).Radius(8).Background(t.Surface).Clip().Children(func() {
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
				ui.Box(c.Key(i)).Background(fill).Padding(8, 12).Children(func() {
					ui.Text(c, prefix+line.Text).FontSize(12.5).LineHeight(1.45).TextColor(ink)
				})
			}
		})
		if ch.Error != "" {
			ui.Text(c, ch.Error).TextColor(t.Danger).FontSize(12)
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ch.Status == "pending" {
				if oneBotApplyButton(c, "accept", i18n.T("应用这处")).Disabled(task.Applying).Clicked() {
					if err := ob.applyOneBotCandidate(task, candidate, index); err != nil {
						ch.Error = err.Error()
						task.Error = err.Error()
					}
				}
				if quietOneBotAction(c, "reject", i18n.T("拒绝这处")).Disabled(task.Applying).Clicked() {
					ch.Status = "rejected"
					ob.keepOneBotTask(task)
				}
			}
			if quietOneBotAction(c, "locate", i18n.T("定位原文")).Clicked() {
				ob.host.openAnywhere(task.SpaceID, ch.Path)
				if ob.host.activeDocument() != nil {
					if start, end, err := ob.host.activeDocument().ed.ResolveEdit(ch.Operation.Target); err == nil {
						ob.host.activeDocument().ed.SetSelection(start, end)
						ob.host.activeDocument().ed.RequestFocus = true
					}
				}
			}
			if ch.Status == "applied" && quietOneBotAction(c, "save", i18n.T("重试保存")).Clicked() {
				ob.retryOneBotSave(task, ch)
			}
		})
	})
}

func (ob *qaState) keepOneBotTask(task *oneBotTask) {
	if err := ob.persistOneBotTask(task); err != nil {
		task.Error = err.Error()
		return
	}
	if s := ob.sessions[task.SpaceID]; s != nil {
		for _, turn := range s.turns {
			if turn.edit == task {
				ob.saveOneBotConversation(task.SpaceID, s)
				break
			}
		}
	}
}
func (ob *qaState) restartOneBotTask(task *oneBotTask) {
	if task.Running {
		return
	}
	ob.keepOneBotTask(task)
	ob.editTask = nil
	ob.editPaths = nil
	seen := map[string]bool{}
	for _, target := range task.Targets {
		if !seen[target.Path] {
			seen[target.Path] = true
			ob.editPaths = append(ob.editPaths, target.Path)
		}
	}
	scope := task.Scope
	if scope == "selection" {
		ob.host.notify("%s", i18n.T("请重新选择需要修改的内容"))
		return
	}
	next, err := ob.captureOneBotTask(scope)
	if err != nil {
		task.Error = err.Error()
		ob.editTask = task
		return
	}
	ob.editTask = next
	ob.input = task.Instruction
	ob.focusInput = true
}

func (ob *qaState) retryOneBotSave(task *oneBotTask, ch *oneBotChange) {
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
		ob.keepOneBotTask(task)
		return
	}
}

func (ob *qaState) revertOneBotCandidate(task *oneBotTask, candidate *oneBotCandidate) {
	seen := map[string]bool{}
	for i := len(candidate.Changes) - 1; i >= 0; i-- {
		ch := candidate.Changes[i]
		if ch.Status != "saved" && ch.Status != "applied" {
			continue
		}
		if ch.Receipt == nil {
			if ch.Operation.Kind == "create_document" {
				if err := ob.revertOneBotCreation(task, ch); err != nil {
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
			if ob.host.openAnywhere(task.SpaceID, ch.Path) {
				d = ob.host.activeDocument()
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
	ob.keepOneBotTask(task)
}

func (ob *qaState) revertOneBotCreation(task *oneBotTask, ch *oneBotChange) error {
	sp := ob.qaSpaceByID(task.SpaceID)
	if sp == nil {
		return errors.New(i18n.T("空间已不存在"))
	}
	for _, d := range ob.host.residentDocuments() {
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
	kept, err := ob.host.trashBin().Add(sp.ID, sp.Name, sp.Root, ch.Path, time.Now())
	if err != nil {
		return err
	}
	if err = ob.host.trashItem(full); err != nil {
		return err
	}
	for _, d := range ob.host.residentDocuments() {
		if d.space.ID == sp.ID && d.rel == ch.Path {
			ob.host.replaceReferences(d, nil)
		}
	}
	ob.host.spaces().Forget(sp.ID, ch.Path)
	ob.host.saveState()
	ob.host.refreshTree()
	ob.host.offerUndo(i18n.T("已移到废纸篓"), ch.Path, func() { ob.host.restoreDeleted(kept) })
	return nil
}

func (ob *qaState) loadOneBotHistory() {
	dir := filepath.Join(ob.host.appConfig().ConfigDir, "onebot", "tasks")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		ob.host.notify("%s", err.Error())
		return
	}
	ob.editHistory = nil
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
		ob.editHistory = append(ob.editHistory, &task)
	}
	slices.SortFunc(ob.editHistory, func(a, b *oneBotTask) int { return b.Created.Compare(a.Created) })
}
func (ob *qaState) oneBotHistoryView(c *ui.Context) {
	t := c.Theme()
	if len(ob.editHistory) == 0 {
		ui.Text(c, i18n.T("暂无编辑记录")).FontSize(12.5).TextColor(t.TextMuted).Padding(8, 0)
		return
	}
	ui.Column(c).Gap(8).Children(func() {
		for _, task := range ob.editHistory {
			ui.Row(c.Key(task.ID)).Gap(12).Padding(12, 12).Radius(8).AlignItems(ui.Center).
				Background(t.Surface).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, task.Instruction).FontSize(12.5).FontWeight(500).SingleLine()
					ui.Text(c, task.Created.Format("2006-01-02 15:04")).FontSize(11).TextColor(t.TextMuted)
				})
				if quietOneBotAction(c, "open", i18n.T("打开记录")).Clicked() {
					ob.editTask = task
					ob.attachOneBotTask(task)
					ob.editHistoryOpen = false
					ob.editScope = task.Scope
					ob.editNewPath = task.NewPath
					task.Stopped = false
				}
				del := ui.Box(c.Key("delete")).Size(26, 26).Radius(8).Center().Shrink(0).
					Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
					Label(i18n.T("删除记录：") + task.Instruction).Tooltip(i18n.T("删除记录"))
				delInk := t.TextMuted.Alpha(0.65)
				if del.Hovered() || del.FocusVisible() {
					del.Background(t.Danger.Alpha(0.12))
					delInk = t.Danger
				}
				del.Children(func() { ui.Icon(c, iconTrash).FontSize(13.5).TextColor(delInk) })
				if del.Clicked() {
					ob.deleteOneBotEditTask(task.ID)
				}
			})
		}
	})
}
