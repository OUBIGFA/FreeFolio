package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo/ui"
)

func TestOneBotHistoryPersistsOpensFollowsUpAndDeletes(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		call(agent.ToolRead, `{"path":"计划.md","start_line":1,"end_line":5}`),
		reply("发布延期是因为支付联调 [计划.md#L3]。"),
		reply("第二轮关于架构的回答。"),
		reply("继续第一轮：支付联调预计周五完成。"),
	}}
	a, tt := qaApp(t, model)
	os.WriteFile(filepath.Join(root(a), "计划.md"), []byte("# 计划\n\n决定延期发布，原因是支付联调。\n"), 0o644)

	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)

	// Ask first conversation.
	a.qa.input = "为什么发布延期？"
	a.askQA()
	spID := a.state.ActiveSpace().ID
	qaSettle(t, tt, a.qaSession(spID).turns[0])
	firstID := a.qaSession(spID).id
	if firstID == "" {
		t.Fatal("first conversation was not assigned an ID")
	}

	// Advance clock so the second conversation sorts strictly newer.
	tt.Advance(2 * time.Minute)
	tt.Frame()

	// Start a new conversation and ask a second question.
	if err := tt.Click("新对话"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.qa.input = "总结一下架构设计"
	a.askQA()
	qaSettle(t, tt, a.qaSession(spID).turns[0])
	secondID := a.qaSession(spID).id
	if secondID == "" || secondID == firstID {
		t.Fatalf("expected distinct second conversation ID, got %q vs %q", secondID, firstID)
	}

	// Open History UI via header button.
	if err := tt.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.qa.historyOpen {
		t.Fatal("history panel did not open")
	}
	for _, want := range []string{"历史记录", "总结一下架构设计", "为什么发布延期？", "当前"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in history panel: %q", want, tt.Texts())
		}
	}

	// Search filters conversations and clearing search restores all.
	a.qa.historyQuery = "延期"
	tt.Frame()
	if !tt.HasText("为什么发布延期？") || tt.HasText("总结一下架构设计") {
		t.Fatalf("search did not filter list: %q", tt.Texts())
	}
	if err := tt.Click("清除搜索"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("总结一下架构设计") || !tt.HasText("为什么发布延期？") {
		t.Fatalf("clearing search did not restore list: %q", tt.Texts())
	}

	// Click the first conversation to restore it, view citations, and ask a follow-up.
	if err := tt.Click("为什么发布延期？"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.qa.historyOpen {
		t.Fatal("opening a conversation did not close history panel")
	}
	if a.qaSession(spID).id != firstID {
		t.Fatalf("active session = %q, want %q", a.qaSession(spID).id, firstID)
	}
	if !tt.HasText("发布延期是因为支付联调") || !tt.HasText("1 个来源") {
		t.Fatalf("restored conversation content missing: %q", tt.Texts())
	}

	// Follow-up in the restored conversation includes prior messages.
	a.qa.input = "那什么时候能完成？"
	a.askQA()
	qaSettle(t, tt, a.qaSession(spID).turns[1])
	lastReq := model.reqs[len(model.reqs)-1]
	if len(lastReq.Messages) < 3 || lastReq.Messages[0].Text != "为什么发布延期？" || lastReq.Messages[len(lastReq.Messages)-1].Text != "那什么时候能完成？" {
		t.Fatalf("follow-up lost prior conversation history: %+v", lastReq.Messages)
	}

	// Open history again: first conversation now has 2 turns and is marked Current.
	if err := tt.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("2 轮对话") {
		t.Fatalf("updated turn count not shown: %q", tt.Texts())
	}

	// Delete the non-active second conversation.
	if err := tt.Click("删除对话：总结一下架构设计"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("总结一下架构设计") {
		t.Fatalf("deleted conversation still visible: %q", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(a.oneBotConversationDir(), secondID+".json")); !os.IsNotExist(err) {
		t.Fatalf("deleted conversation file still exists: %v", err)
	}
	// Active conversation remains intact.
	if a.qaSession(spID).id != firstID {
		t.Fatalf("deleting other conversation affected active session: %q", a.qaSession(spID).id)
	}

	// Delete the currently active first conversation.
	if err := tt.Click("删除对话：为什么发布延期？"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("还没有历史对话") {
		t.Fatalf("expected empty history state after deleting last item: %q", tt.Texts())
	}
	if len(a.qaSession(spID).turns) != 0 || a.qaSession(spID).id != "" {
		t.Fatalf("deleting active conversation did not reset active session: %+v", a.qaSession(spID))
	}
	if _, err := os.Stat(filepath.Join(a.oneBotConversationDir(), firstID+".json")); !os.IsNotExist(err) {
		t.Fatalf("active conversation file still exists after delete: %v", err)
	}
}

func TestOneBotHistorySurvivesRestartAndIsolatesSpaces(t *testing.T) {
	cfgDir := t.TempDir()
	docsDir := t.TempDir()
	spaceRootB := t.TempDir()
	os.WriteFile(filepath.Join(spaceRootB, "NoteB.md"), []byte("# Note B\n\nBeta content.\n"), 0o644)

	makeApp := func(m *scripted) (*App, *ui.Tester, string, string) {
		a, err := New(Config{ConfigDir: cfgDir, DocumentsDir: docsDir})
		if err != nil {
			t.Fatal(err)
		}
		a.tour = tour{}
		a.tourSeen()
		if len(a.state.Spaces) < 2 {
			spB := &space.Space{ID: "space-b", Name: "Work", Root: spaceRootB}
			a.state.Spaces = append(a.state.Spaces, spB)
			a.saveState()
		}
		os.WriteFile(filepath.Join(a.state.Spaces[0].Root, "NoteA.md"), []byte("# Note A\n\nAlpha content.\n"), 0o644)
		setupOneBot(a, "sk-test")
		a.qa.sync = true
		a.qa.provider = func(agent.Settings) agent.Provider { return m }
		tt := ui.NewTester(a.View, 1280, 820)
		tt.SetLocale("zh-Hans")
		tt.Frame()
		return a, tt, a.state.Spaces[0].ID, a.state.Spaces[1].ID
	}

	// First session: create conversations in Space A and Space B.
	m1 := &scripted{steps: []func(agent.Request) agent.Message{
		reply("来自空间 A 的回答 [NoteA.md#L3]"),
		reply("来自空间 B 的回答 [NoteB.md#L3]"),
	}}
	a1, tt1, idA, idB := makeApp(m1)
	a1.toggleQA()
	tt1.Frame()
	tt1.Advance(slide)
	a1.qa.input = "空间 A 的问题"
	a1.askQA()
	qaSettle(t, tt1, a1.qaSession(idA).turns[0])

	a1.switchSpace(idB)
	tt1.Frame()
	a1.qa.input = "空间 B 的问题"
	a1.askQA()
	qaSettle(t, tt1, a1.qaSession(idB).turns[0])

	// Simulated restart: fresh App reading same ConfigDir.
	a2, tt2, idA2, idB2 := makeApp(&scripted{})
	if idA2 != idA || idB2 != idB {
		t.Fatalf("space IDs drifted across restart")
	}
	a2.switchSpace(idA2)
	a2.toggleQA()
	tt2.Frame()
	tt2.Advance(slide)

	if err := tt2.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	if !tt2.HasText("空间 A 的问题") || tt2.HasText("空间 B 的问题") {
		t.Fatalf("space A history not isolated after restart: %q", tt2.Texts())
	}

	// Open the persisted conversation and click its citation before asking anything new.
	if err := tt2.Click("空间 A 的问题"); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	if !tt2.HasText("来自空间 A 的回答") {
		t.Fatalf("reloaded answer missing: %q", tt2.Texts())
	}
	if err := tt2.Click("1 个来源"); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	tt2.Advance(240 * time.Millisecond)
	tt2.Frame()
	if err := tt2.Click("1 Note A 第 3 行"); err != nil {
		t.Fatalf("%v; texts=%q", err, tt2.Texts())
	}
	tt2.Frame()
	if a2.doc == nil || a2.doc.rel != "NoteA.md" {
		t.Fatalf("citation in reloaded history did not open NoteA.md: %+v", a2.doc)
	}

	// Clear all in Space A should keep Space B's history intact.
	if err := tt2.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	if err := tt2.Click("清空"); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	if err := tt2.Click("确认清空"); err != nil {
		t.Fatal(err)
	}
	tt2.Frame()
	if !tt2.HasText("还没有历史对话") {
		t.Fatalf("space A history not cleared: %q", tt2.Texts())
	}

	a2.switchSpace(idB2)
	tt2.Frame()
	if !tt2.HasText("空间 B 的问题") {
		t.Fatalf("clearing space A erased space B history: %q", tt2.Texts())
	}
}

func TestOneBotHistoryEscapeAndSafeIDsAndEditTaskDelete(t *testing.T) {
	a, tt := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{reply("ok")}})
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)

	// Path traversal IDs are ignored safely.
	for _, bad := range []string{"", ".", "..", "../escape", "a/b", `a\b`, "bad.json"} {
		if isSafeOneBotID(bad) {
			t.Fatalf("unsafe ID %q accepted", bad)
		}
	}

	// Open history, press Escape to close.
	if err := tt.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.qa.historyOpen {
		t.Fatal("history did not open")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.qa.historyOpen {
		t.Fatal("Escape did not close history")
	}

	// Edit history drawer supports deleting a persisted edit task.
	task := editTestTask(t, a)
	task.Instruction = "优化这段文字"
	if err := a.persistOneBotTask(task); err != nil {
		t.Fatal(err)
	}
	a.loadOneBotHistory()
	a.qa.editHistoryOpen = true
	tt.Frame()
	if !tt.HasText("优化这段文字") {
		t.Fatalf("edit task missing in drawer: %q", tt.Texts())
	}
	if err := tt.Click("删除记录：优化这段文字"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("优化这段文字") || !tt.HasText("暂无编辑记录") {
		t.Fatalf("deleted edit task still visible: %q", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(a.cfg.ConfigDir, "onebot", "tasks", task.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("deleted edit task file still exists: %v", err)
	}
}

func TestOneBotHistorySanitizesBinaryAttachments(t *testing.T) {
	msgs := []agent.Message{
		{
			Role: agent.User,
			Text: "看这张图",
			Parts: []agent.ContentPart{
				{Type: "text", Text: "Attached file: a.txt\nhello", Name: "a.txt"},
				{Type: "image", Data: "data:image/png;base64,AAAA", MIME: "image/png", Name: "pic.png"},
			},
		},
	}
	got := sanitizeOneBotHistoryMessages(msgs)
	if len(got) != 1 || len(got[0].Parts) != 1 || got[0].Parts[0].Type != "text" || !strings.Contains(got[0].Parts[0].Text, "hello") {
		t.Fatalf("unexpected sanitized messages: %+v", got)
	}
}

func TestOneBotHistorySnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect OneBot history")
	for _, mode := range []string{"light", "dark", "empty", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			model := &scripted{steps: []func(agent.Request) agent.Message{
				reply("发布延期是因为支付联调与安全证书审批仍在走流程。"),
				reply("最近修改了项目发布计划、接口设计文档和周报，重点围绕稳定性改进。"),
				reply("当前有 3 项未完成任务：补齐回调测试、核对发布清单、更新用户指南。"),
			}}
			a, tt := qaApp(t, model)
			if mode == "dark" {
				tt.SetDark(true)
			}
			if mode == "narrow" {
				tt.SetSize(720, 560)
			}
			a.toggleQA()
			tt.Frame()
			tt.Advance(slide)
			if mode != "empty" {
				spID := a.state.ActiveSpace().ID
				for i, q := range []string{"为什么发布延期？", "总结一下最近修改的几篇笔记", "这个空间里有哪些还没完成的任务？"} {
					if i > 0 {
						tt.Advance(25 * time.Minute)
						tt.Frame()
						a.clearQA()
					}
					a.qa.input = q
					a.askQA()
					tt.Frame()
					qaSettle(t, tt, a.qaSession(spID).turns[0])
				}
			}
			if err := tt.Click("历史记录"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "onebot-history-"+mode+".png"), tt)
		})
	}
}
