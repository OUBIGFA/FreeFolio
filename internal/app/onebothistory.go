package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/fsutil"
	"github.com/OUBIGFA/FreeFolio/internal/i18n"
	"github.com/egoist/mygo/ui"
)

const (
	oneBotConversationVersion  = 1
	oneBotMaxConversationBytes = 32 << 20
)

type oneBotConversation struct {
	Version      int               `json:"version"`
	ID           string            `json:"id"`
	SpaceID      string            `json:"space"`
	Title        string            `json:"title"`
	Preview      string            `json:"preview,omitempty"`
	Created      time.Time         `json:"created"`
	Updated      time.Time         `json:"updated"`
	Source       string            `json:"source,omitempty"`
	ContextStart int               `json:"contextStart,omitempty"`
	History      []agent.Message   `json:"history,omitempty"`
	Turns        []oneBotSavedTurn `json:"turns"`
}

type oneBotSavedTurn struct {
	Model        string                  `json:"model,omitempty"`
	Question     string                  `json:"question"`
	Quotes       []oneBotQuote           `json:"quotes,omitempty"`
	Attachments  []oneBotAttachmentBadge `json:"attachments,omitempty"`
	Start        int                     `json:"start"`
	ContextBreak bool                    `json:"contextBreak,omitempty"`
	Steps        []oneBotSavedStep       `json:"steps,omitempty"`
	Answer       string                  `json:"answer,omitempty"`
	FinishReason string                  `json:"finishReason,omitempty"`
	Stopped      bool                    `json:"stopped,omitempty"`
	Error        string                  `json:"error,omitempty"`
	EditTaskID   string                  `json:"editTaskId,omitempty"`
}

type oneBotSavedStep struct {
	Call   agent.ToolCall `json:"call"`
	Done   bool           `json:"done"`
	Failed bool           `json:"failed,omitempty"`
	Result string         `json:"result,omitempty"`
}

type oneBotConversationSummary struct {
	ID        string
	SpaceID   string
	Title     string
	Preview   string
	TurnCount int
	Created   time.Time
	Updated   time.Time
}

var (
	oneBotCiteTagRe = regexp.MustCompile(`\[[^\[\]\n]+#L\d+(?:-\d+)?\]`)
	oneBotMdMarkRe  = regexp.MustCompile("[#*`>_~]+")
)

func isSafeOneBotID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := range len(id) {
		ch := id[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

func (ob *qaState) clockNow() time.Time {
	if !ob.host.clock().IsZero() {
		return ob.host.clock()
	}
	return time.Now()
}

func (ob *qaState) oneBotConversationDir() string {
	if ob.host.appConfig().ConfigDir == "" {
		return ""
	}
	return filepath.Join(ob.host.appConfig().ConfigDir, "onebot", "conversations")
}

func collapseSingleLine(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

func oneBotConversationTitle(turns []*qaTurn) string {
	for _, turn := range turns {
		if q := collapseSingleLine(turn.question, 120); q != "" {
			return q
		}
		if len(turn.attachments) > 0 {
			names := make([]string, 0, len(turn.attachments))
			for _, att := range turn.attachments {
				if att.Name != "" {
					names = append(names, att.Name)
				}
			}
			if q := collapseSingleLine(strings.Join(names, ", "), 120); q != "" {
				return q
			}
		}
	}
	return ""
}

func oneBotConversationPreview(turns []*qaTurn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		raw := strings.TrimSpace(turn.answer)
		if raw == "" && turn.edit != nil && turn.edit.Selected >= 0 && turn.edit.Selected < len(turn.edit.Candidates) {
			raw = strings.TrimSpace(turn.edit.Candidates[turn.edit.Selected].Text)
		}
		if raw == "" {
			continue
		}
		cleaned := oneBotCiteTagRe.ReplaceAllString(raw, "")
		cleaned = oneBotMdMarkRe.ReplaceAllString(cleaned, " ")
		if preview := collapseSingleLine(cleaned, 120); preview != "" {
			return preview
		}
	}
	return ""
}

func sanitizeOneBotHistoryMessages(msgs []agent.Message) []agent.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]agent.Message, len(msgs))
	for i, m := range msgs {
		copied := m
		if len(m.Calls) > 0 {
			copied.Calls = append([]agent.ToolCall(nil), m.Calls...)
		}
		if len(m.Parts) > 0 {
			var parts []agent.ContentPart
			for _, p := range m.Parts {
				// Drop large inline binary payloads so saved history files stay
				// compact and remain valid when replayed in follow-up turns.
				if p.Data != "" {
					continue
				}
				parts = append(parts, p)
			}
			copied.Parts = parts
		}
		out[i] = copied
	}
	return out
}

func sortOneBotSummaries(list []oneBotConversationSummary) {
	slices.SortFunc(list, func(x, y oneBotConversationSummary) int {
		if cmp := y.Updated.Compare(x.Updated); cmp != 0 {
			return cmp
		}
		if cmp := y.Created.Compare(x.Created); cmp != 0 {
			return cmp
		}
		return strings.Compare(y.ID, x.ID)
	})
}

func (ob *qaState) upsertOneBotHistorySummary(sum oneBotConversationSummary) {
	for i := range ob.history {
		if ob.history[i].ID == sum.ID {
			ob.history[i] = sum
			sortOneBotSummaries(ob.history)
			return
		}
	}
	ob.history = append(ob.history, sum)
	sortOneBotSummaries(ob.history)
}

func (ob *qaState) saveOneBotConversation(spaceID string, s *qaSession) {
	if s == nil || s.deleted || len(s.turns) == 0 || spaceID == "" {
		return
	}
	dir := ob.oneBotConversationDir()
	if dir == "" {
		return
	}
	now := ob.clockNow()
	if s.id == "" {
		s.id = model.NewID()
		s.created = now
	}
	if s.created.IsZero() {
		s.created = now
	}
	s.spaceID = spaceID
	s.updated = now

	savedTurns := make([]oneBotSavedTurn, len(s.turns))
	for i, turn := range s.turns {
		st := oneBotSavedTurn{
			Model:        turn.model,
			Question:     turn.question,
			Quotes:       append([]oneBotQuote(nil), turn.quotes...),
			Attachments:  append([]oneBotAttachmentBadge(nil), turn.attachments...),
			Start:        turn.start,
			ContextBreak: turn.contextBreakAfter,
			Answer:       turn.answer,
			FinishReason: turn.finishReason,
			Stopped:      turn.stopped,
		}
		if turn.err != nil {
			st.Error = turn.err.Error()
		}
		if turn.edit != nil && isSafeOneBotID(turn.edit.ID) {
			st.EditTaskID = turn.edit.ID
		}
		if len(turn.steps) > 0 {
			st.Steps = make([]oneBotSavedStep, len(turn.steps))
			for j, step := range turn.steps {
				st.Steps[j] = oneBotSavedStep{
					Call:   step.call,
					Done:   step.done,
					Failed: step.failed,
					Result: step.result,
				}
			}
		}
		savedTurns[i] = st
	}

	conv := oneBotConversation{
		Version:      oneBotConversationVersion,
		ID:           s.id,
		SpaceID:      spaceID,
		Title:        oneBotConversationTitle(s.turns),
		Preview:      oneBotConversationPreview(s.turns),
		Created:      s.created,
		Updated:      s.updated,
		Source:       s.source,
		ContextStart: s.contextStart,
		History:      sanitizeOneBotHistoryMessages(s.history),
		Turns:        savedTurns,
	}
	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(dir, s.id+".json"), data, 0600); err != nil {
		return
	}
	ob.upsertOneBotHistorySummary(oneBotConversationSummary{
		ID:        conv.ID,
		SpaceID:   conv.SpaceID,
		Title:     conv.Title,
		Preview:   conv.Preview,
		TurnCount: len(conv.Turns),
		Created:   conv.Created,
		Updated:   conv.Updated,
	})
}

func readOneBotConversationSummaries(dir string) ([]oneBotConversationSummary, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []oneBotConversationSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() > oneBotMaxConversationBytes {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var conv oneBotConversation
		if json.Unmarshal(data, &conv) != nil || conv.Version != oneBotConversationVersion || !isSafeOneBotID(conv.ID) || len(conv.Turns) == 0 {
			continue
		}
		updated := conv.Updated
		if updated.IsZero() {
			updated = conv.Created
		}
		out = append(out, oneBotConversationSummary{
			ID:        conv.ID,
			SpaceID:   conv.SpaceID,
			Title:     conv.Title,
			Preview:   conv.Preview,
			TurnCount: len(conv.Turns),
			Created:   conv.Created,
			Updated:   updated,
		})
	}
	sortOneBotSummaries(out)
	return out, nil
}

func (ob *qaState) toggleOneBotHistory() {
	ob.historyOpen = !ob.historyOpen
	ob.historyConfirmClear = false
	if ob.historyOpen {
		ob.editHistoryOpen = false
		ob.editOptionsOpen = false
		ob.loadOneBotConversations()
	}
}

func (ob *qaState) loadOneBotConversations() {
	dir := ob.oneBotConversationDir()
	ob.historyEpoch++
	epoch := ob.historyEpoch
	ob.historyErr = nil
	if dir == "" {
		ob.history = nil
		ob.historyLoaded = true
		ob.historyLoading = false
		return
	}
	if ob.host.window() != nil && !ob.sync {
		ob.historyLoading = true
		win := ob.host.window()
		go func() {
			list, err := readOneBotConversationSummaries(dir)
			win.Update(func() {
				if ob.historyEpoch != epoch {
					return
				}
				ob.historyLoading = false
				ob.historyLoaded = true
				ob.historyErr = err
				if err == nil {
					ob.history = list
				}
			})
		}()
		return
	}
	list, err := readOneBotConversationSummaries(dir)
	ob.historyLoading = false
	ob.historyLoaded = true
	ob.historyErr = err
	if err == nil {
		ob.history = list
	}
}

func (ob *qaState) loadOneBotTaskByID(id string) *oneBotTask {
	if !isSafeOneBotID(id) || ob.host.appConfig().ConfigDir == "" {
		return nil
	}
	p := filepath.Join(ob.host.appConfig().ConfigDir, "onebot", "tasks", id+".json")
	info, err := os.Stat(p)
	if err != nil || info.IsDir() || info.Size() > oneBotMaxConversationBytes {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var task oneBotTask
	if json.Unmarshal(data, &task) != nil || task.Version != 1 {
		return nil
	}
	for _, candidate := range task.Candidates {
		for _, ch := range candidate.Changes {
			ch.Receipt = task.Receipts[ch.Batch]
		}
	}
	return &task
}

func (ob *qaState) openOneBotConversation(spaceID, id string) {
	if !isSafeOneBotID(id) || spaceID == "" {
		return
	}
	cur := ob.qaSession(spaceID)
	if cur.id == id && len(cur.turns) > 0 {
		ob.historyOpen = false
		ob.historyConfirmClear = false
		return
	}
	dir := ob.oneBotConversationDir()
	if dir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		ob.host.notify("%s", i18n.T("无法打开历史对话：")+err.Error())
		if errors.Is(err, os.ErrNotExist) {
			ob.history = slices.DeleteFunc(ob.history, func(s oneBotConversationSummary) bool { return s.ID == id })
		}
		return
	}
	var conv oneBotConversation
	if err := json.Unmarshal(data, &conv); err != nil || conv.Version != oneBotConversationVersion || len(conv.Turns) == 0 {
		ob.host.notify("%s", i18n.T("无法打开历史对话：记录格式无效"))
		return
	}

	// Preserve and stop the current conversation before switching.
	if cur.cancel != nil {
		cur.cancel()
		if len(cur.turns) > 0 && cur.turns[len(cur.turns)-1].running {
			cur.turns[len(cur.turns)-1].running = false
			cur.turns[len(cur.turns)-1].stopped = true
		}
	}
	if len(cur.turns) > 0 {
		ob.saveOneBotConversation(spaceID, cur)
	}
	if ob.editTask != nil && (ob.editTask.Running || ob.editTask.Applying) {
		ob.stopOneBotEdit()
	}

	turns := make([]*qaTurn, len(conv.Turns))
	for i, st := range conv.Turns {
		turn := &qaTurn{
			model:             st.Model,
			question:          st.Question,
			quotes:            append([]oneBotQuote(nil), st.Quotes...),
			attachments:       append([]oneBotAttachmentBadge(nil), st.Attachments...),
			start:             st.Start,
			contextBreakAfter: st.ContextBreak,
			answer:            st.Answer,
			finishReason:      st.FinishReason,
			stopped:           st.Stopped,
		}
		if st.Error != "" {
			turn.err = errors.New(st.Error)
		}
		if len(st.Steps) > 0 {
			turn.steps = make([]*qaStep, len(st.Steps))
			for j, step := range st.Steps {
				turn.steps[j] = &qaStep{
					call:   step.Call,
					done:   step.Done,
					failed: step.Failed,
					result: step.Result,
				}
			}
		}
		if st.EditTaskID != "" {
			turn.edit = ob.loadOneBotTaskByID(st.EditTaskID)
		}
		turn.stream.sync(turn.answer, ob.clockNow())
		turns[i] = turn
	}

	loaded := &qaSession{
		id:           conv.ID,
		spaceID:      spaceID,
		created:      conv.Created,
		updated:      conv.Updated,
		source:       conv.Source,
		contextStart: min(max(conv.ContextStart, 0), len(conv.History)),
		history:      append([]agent.Message(nil), conv.History...),
		turns:        turns,
		list:         ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth},
	}
	if loaded.contextStart == 0 {
		recomputeContextStart(loaded)
	}
	ob.sessions[spaceID] = loaded
	ob.editTask = nil
	ob.editMode = false
	ob.editFollow = false
	ob.attachments = nil
	ob.quotes = nil
	ob.historyOpen = false
	ob.historyConfirmClear = false
	ob.focusInput = true
}

func (ob *qaState) deleteOneBotConversation(spaceID, id string) {
	if !isSafeOneBotID(id) {
		return
	}
	if dir := ob.oneBotConversationDir(); dir != "" {
		if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			ob.host.notify("%s", i18n.T("无法删除历史记录：")+err.Error())
			return
		}
	}
	if cur := ob.sessions[spaceID]; cur != nil && cur.id == id {
		if cur.cancel != nil {
			cur.cancel()
		}
		cur.deleted = true
		ob.sessions[spaceID] = &qaSession{spaceID: spaceID, list: ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth}}
		if sp := ob.qaActiveSpace(); sp != nil && sp.ID == spaceID {
			if ob.editTask != nil && (ob.editTask.Running || ob.editTask.Applying) {
				ob.stopOneBotEdit()
			}
			ob.editTask = nil
			ob.editMode = false
			ob.editFollow = false
			ob.attachments = nil
			ob.quotes = nil
		}
	}
	ob.history = slices.DeleteFunc(ob.history, func(s oneBotConversationSummary) bool { return s.ID == id })
}

func (ob *qaState) clearOneBotConversations(spaceID string) {
	if spaceID == "" {
		return
	}
	dir := ob.oneBotConversationDir()
	seen := map[string]bool{}
	var ids []string
	for _, item := range ob.history {
		if item.SpaceID == spaceID && isSafeOneBotID(item.ID) && !seen[item.ID] {
			seen[item.ID] = true
			ids = append(ids, item.ID)
		}
	}
	if cur := ob.sessions[spaceID]; cur != nil && isSafeOneBotID(cur.id) && !seen[cur.id] {
		seen[cur.id] = true
		ids = append(ids, cur.id)
	}
	deleted := map[string]bool{}
	var firstErr error
	for _, id := range ids {
		if dir != "" {
			if err := os.Remove(filepath.Join(dir, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		deleted[id] = true
	}
	if cur := ob.sessions[spaceID]; cur != nil && (cur.id == "" || deleted[cur.id]) {
		if cur.cancel != nil {
			cur.cancel()
		}
		cur.deleted = true
		ob.sessions[spaceID] = &qaSession{spaceID: spaceID, list: ui.ListState{FollowEnd: true, FollowAnimation: &ui.Smooth}}
		if sp := ob.qaActiveSpace(); sp != nil && sp.ID == spaceID {
			if ob.editTask != nil && (ob.editTask.Running || ob.editTask.Applying) {
				ob.stopOneBotEdit()
			}
			ob.editTask = nil
			ob.editMode = false
			ob.editFollow = false
			ob.attachments = nil
			ob.quotes = nil
		}
	}
	ob.history = slices.DeleteFunc(ob.history, func(s oneBotConversationSummary) bool { return deleted[s.ID] })
	ob.historyConfirmClear = false
	ob.historyQuery = ""
	if firstErr != nil {
		ob.host.notify("%s", i18n.T("无法删除历史记录：")+firstErr.Error())
	}
}

func (ob *qaState) deleteOneBotEditTask(id string) {
	if !isSafeOneBotID(id) {
		return
	}
	if ob.host.appConfig().ConfigDir != "" {
		p := filepath.Join(ob.host.appConfig().ConfigDir, "onebot", "tasks", id+".json")
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			ob.host.notify("%s", i18n.T("无法删除历史记录：")+err.Error())
			return
		}
	}
	ob.editHistory = slices.DeleteFunc(ob.editHistory, func(t *oneBotTask) bool { return t.ID == id })
}

func (ob *qaState) oneBotHistoryItems(spaceID, query string) (allCount int, filtered []oneBotConversationSummary) {
	q := strings.ToLower(strings.TrimSpace(query))
	for _, item := range ob.history {
		if item.SpaceID != spaceID {
			continue
		}
		allCount++
		if q == "" {
			filtered = append(filtered, item)
			continue
		}
		title := item.Title
		if title == "" {
			title = i18n.T("未命名对话")
		}
		if strings.Contains(strings.ToLower(title), q) || strings.Contains(strings.ToLower(item.Preview), q) {
			filtered = append(filtered, item)
		}
	}
	return allCount, filtered
}

func (ob *qaState) oneBotHistoryPanel(c *ui.Context, spaceID string, s *qaSession) {
	t := c.Theme()
	if c.Shortcut(0, ui.KeyEscape) {
		switch {
		case ob.historyConfirmClear:
			ob.historyConfirmClear = false
		case ob.historyQuery != "":
			ob.historyQuery = ""
		default:
			ob.historyOpen = false
			return
		}
	}

	total, items := ob.oneBotHistoryItems(spaceID, ob.historyQuery)

	ui.Column(c.Key("qa-history-panel")).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c.Key("qa-history-head")).Padding(8, 10, 8, 8).Gap(6).AlignItems(ui.Center).Children(func() {
			if iconButton(c, "qa-history-back", iconBack, i18n.T("返回对话"), i18n.T("返回对话"), false).Size(26, 26).Clicked() {
				ob.historyOpen = false
				ob.historyConfirmClear = false
			}
			ui.Text(c, i18n.T("历史记录")).FontSize(13).FontWeight(600).NoWrap()
			if total > 0 {
				ui.Box(c.Key("qa-history-count")).Padding(1, 6).Radius(9).Background(t.Text.Alpha(0.06)).Shrink(0).Children(func() {
					ui.Text(c, fmt.Sprint(total)).FontSize(11).FontWeight(500).TextColor(t.TextMuted).NoWrap()
				})
			}
			ui.Spacer(c)
			if total > 0 && !ob.historyLoading {
				if ob.historyConfirmClear {
					if quietOneBotAction(c, "qa-history-clear-cancel", i18n.T("取消")).Clicked() {
						ob.historyConfirmClear = false
					}
					confirm := ui.Row(c.Key("qa-history-clear-confirm")).Height(26).Padding(2, 9).Radius(5).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("确认清空"))
					fill := t.Danger.Alpha(0.12)
					if confirm.Hovered() {
						fill = t.Danger.Alpha(0.20)
					}
					confirm.Background(fill).Children(func() {
						ui.Text(c, i18n.T("确认清空")).FontSize(12).FontWeight(600).TextColor(t.Danger).NoWrap()
					})
					if confirm.Clicked() {
						ob.clearOneBotConversations(spaceID)
					}
				} else {
					clr := ui.Row(c.Key("qa-history-clear")).Height(26).Padding(2, 6).Radius(5).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("清空"))
					ink := t.TextMuted
					if clr.Hovered() {
						ink = t.Danger
						clr.Background(t.Danger.Alpha(0.08))
					}
					clr.Children(func() {
						ui.Text(c, i18n.T("清空")).FontSize(12).TextColor(ink).NoWrap()
					})
					if clr.Clicked() {
						ob.historyConfirmClear = true
					}
				}
			}
		})
		ui.Box(c).Height(1).Background(separator(t))

		if ob.historyLoading {
			ui.Column(c.Key("qa-history-loading")).Grow(1).Center().Gap(10).Padding(20).Children(func() {
				ui.Spinner(c).Size(18, 18)
				ui.Text(c, i18n.T("正在加载历史记录")).FontSize(12.5).TextColor(t.TextMuted)
			})
			return
		}

		if ob.historyErr != nil {
			ui.Column(c.Key("qa-history-error")).Grow(1).Center().Gap(10).Padding(20).Children(func() {
				ui.Column(c).Gap(8).Padding(12, 14).Radius(10).AlignItems(ui.Center).MaxWidth(280).
					Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.22)).Children(func() {
					ui.Icon(c, iconAlert).FontSize(16).TextColor(t.Danger)
					ui.Text(c, i18n.T("无法读取历史记录：")+ob.historyErr.Error()).FontSize(12).LineHeight(1.45).TextColor(t.Danger).TextAlign(ui.Center)
					if ui.Button(c.Key("qa-history-retry"), i18n.T("重试")).Clicked() {
						ob.loadOneBotConversations()
					}
				})
			})
			return
		}

		if total == 0 {
			ui.Column(c.Key("qa-history-empty")).Grow(1).Center().Gap(10).Padding(24, 18).Children(func() {
				ui.Box(c.Key("qa-history-empty-badge")).Size(40, 40).Radius(11).Background(t.Text.Alpha(0.05)).
					Border(1, t.Border.Alpha(0.45)).Center().Children(func() {
					ui.Icon(c, iconHistory).FontSize(18).TextColor(t.TextMuted)
				})
				ui.Text(c, i18n.T("还没有历史对话")).FontSize(14).FontWeight(600)
				ui.Text(c, i18n.T("提问后对话会自动保存在这里，方便随时查看、继续或删除")).
					FontSize(12).LineHeight(1.5).TextColor(t.TextMuted).TextAlign(ui.Center).MaxWidth(260)
			})
			return
		}

		ui.Row(c.Key("qa-history-search-wrap")).Padding(8, 12, 4, 12).Children(func() {
			box := ui.Row(c.Key("qa-history-search-box")).Grow(1).MinWidth(0).Height(32).Padding(0, 8).Gap(6).Radius(8).
				AlignItems(ui.Center).Background(field(t)).Border(1, t.Border.Alpha(0.75))
			if box.FocusWithin() {
				box.Border(1, t.Accent.Alpha(0.45))
			}
			box.Children(func() {
				ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted).Shrink(0)
				ui.TextInput(c.Key("qa-history-search"), &ob.historyQuery).Grow(1).MinWidth(0).
					Border(0, t.Border).Background(ui.Color{}).Padding(0).
					Label(i18n.T("搜索历史对话")).Placeholder(i18n.T("搜索历史对话"))
				if ob.historyQuery != "" {
					clr := ui.Box(c.Key("qa-history-search-clear")).Size(20, 20).Radius(5).Center().Shrink(0).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(i18n.T("清除搜索"))
					if clr.Hovered() {
						clr.Background(hoverFill(t))
					}
					clr.Children(func() { ui.Icon(c, iconX).FontSize(11).TextColor(t.TextMuted) })
					if clr.Clicked() {
						ob.historyQuery = ""
					}
				}
			})
		})

		if len(items) == 0 {
			ui.Column(c.Key("qa-history-nomatch")).Grow(1).Center().Gap(8).Padding(20).Children(func() {
				ui.Text(c, i18n.T("没有匹配的历史对话")).FontSize(13).TextColor(t.TextMuted)
				if quietOneBotAction(c, "qa-history-reset-search", i18n.T("清除搜索")).Clicked() {
					ob.historyQuery = ""
				}
			})
			return
		}

		list := ui.List(c.Key("qa-history-list-"+spaceID), &ob.historyList, len(items)).
			Grow(1).MinHeight(0).Padding(6, 12, 12, 12).Label(i18n.T("历史记录")).
			ItemKey(func(i int) any { return items[i].ID })
		list.Rows(func(row ui.ListRow) {
			c := row.Context
			item := items[row.Index]
			isCurrent := s != nil && s.id == item.ID && len(s.turns) > 0
			title := item.Title
			if title == "" {
				title = i18n.T("未命名对话")
			}

			ui.Column(c.Key("item-pad")).Padding(0, 0, 6, 0).Children(func() {
				card := ui.Row(c.Key("card")).Gap(4).Padding(4, 6, 4, 4).Radius(10).AlignItems(ui.Center)
				bg, border := t.Text.Alpha(0.025), t.Border.Alpha(0.45)
				if isCurrent {
					bg, border = t.Accent.Alpha(0.07), t.Accent.Alpha(0.28)
				} else if card.Hovered() {
					bg, border = hoverFill(t), t.Border.Alpha(0.75)
				}
				card.Background(bg).Border(1, border).Children(func() {
					body := ui.Column(c.Key("open")).Grow(1).MinWidth(0).Gap(3).Padding(5, 6).Radius(7).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(title)
					body.Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, title).FontSize(12.5).FontWeight(550).SingleLine().Shrink(1).MinWidth(0)
							if isCurrent {
								ui.Box(c.Key("current-badge")).Padding(1, 6).Radius(4).Background(t.Accent.Alpha(0.14)).Shrink(0).Children(func() {
									ui.Text(c, i18n.T("当前")).FontSize(10).FontWeight(600).TextColor(t.Accent).NoWrap()
								})
							}
						})
						if item.Preview != "" {
							ui.Text(c, item.Preview).FontSize(11.5).TextColor(t.TextMuted).SingleLine().MinWidth(0)
						}
						meta := fmt.Sprintf(i18n.T("%s · %d 轮对话"), versionTime(item.Updated, c.Now()), item.TurnCount)
						ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted.Alpha(0.8)).SingleLine()
					})
					if body.Clicked() {
						ob.openOneBotConversation(spaceID, item.ID)
					}

					del := ui.Box(c.Key("delete")).Size(28, 28).Radius(7).Center().Shrink(0).
						Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).
						Label(i18n.T("删除对话：") + title).Tooltip(i18n.T("删除对话"))
					delInk := t.TextMuted.Alpha(0.65)
					if del.Hovered() || del.FocusVisible() {
						del.Background(t.Danger.Alpha(0.12))
						delInk = t.Danger
					}
					del.Children(func() {
						ui.Icon(c, iconTrash).FontSize(14).TextColor(delInk)
					})
					if del.Clicked() {
						ob.deleteOneBotConversation(spaceID, item.ID)
					}
				})
			})
		})
	})
}
