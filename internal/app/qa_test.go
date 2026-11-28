package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/egoist/mygo/ui"
)

// memSecrets is a credential store in memory.
type memSecrets map[string][]byte

func (m memSecrets) Get(k string) ([]byte, error) {
	if v, ok := m[k]; ok {
		return v, nil
	}
	return nil, os.ErrNotExist
}
func (m memSecrets) Set(k string, v []byte) error { m[k] = v; return nil }
func (m memSecrets) Delete(k string) error        { delete(m, k); return nil }

// scripted is a model replying in turn with the steps given, keeping what
// it was asked.
type scripted struct {
	mu    sync.Mutex
	steps []func(agent.Request) agent.Message
	reqs  []agent.Request
}

func (s *scripted) Complete(_ context.Context, req agent.Request, onDelta func(agent.Delta)) (agent.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
	if len(s.reqs) > len(s.steps) {
		return agent.Response{}, &agent.APIError{Status: 400, Message: "no more steps"}
	}
	m := s.steps[len(s.reqs)-1](req)
	if m.Text != "" {
		onDelta(agent.Delta{Text: m.Text})
	}
	finish := "stop"
	if len(m.Calls) > 0 {
		finish = "tool_calls"
	}
	return agent.Response{Message: m, FinishReason: finish}, nil
}

func call(name, args string) func(agent.Request) agent.Message {
	return func(agent.Request) agent.Message {
		return agent.Message{Role: agent.Assistant, Calls: []agent.ToolCall{{ID: name, Name: name, Args: args}}}
	}
}

func reply(text string) func(agent.Request) agent.Message {
	return func(agent.Request) agent.Message { return agent.Message{Role: agent.Assistant, Text: text} }
}

// qaApp is a test app with a model set up and the given model.
func qaApp(t *testing.T, model *scripted) (*App, *ui.Tester) {
	a, tt := newTestApp(t)
	setupOneBot(a, "sk-test")
	a.qa.sync = true
	a.qa.provider = func(s agent.Settings) agent.Provider {
		if s.APIKey != "sk-test" || s.Model != "test-model" || s.BaseURL != "https://api.openai.com/v1" {
			t.Errorf("settings %+v", s)
		}
		return model
	}
	return a, tt
}

func TestQAPanelOpensAndAsksToSetUp(t *testing.T) {
	a, tt := newTestApp(t)
	a.qa.secrets = memSecrets{}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Frame()
	tt.Advance(slide)
	if !a.qa.open {
		t.Fatal("⌘J did not open the panel")
	}
	if !tt.HasText("向「Personal」提问") || !tt.HasText("设置模型") || !tt.HasText("请先添加供应商") {
		t.Fatalf("panel: %q", tt.Texts())
	}
	if !tt.Focused("问题") {
		t.Fatal("the question field does not have the caret")
	}
	// Asking anyway explains what is missing.
	tt.Type("什么？")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("请先添加供应商") || !tt.HasText("重试") {
		t.Fatalf("unset key: %q", tt.Texts())
	}
	if err := tt.Click("设置模型"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settingsOpen || a.settingsTab != tabAI || !tt.HasText("添加供应商") {
		t.Fatalf("settings: %q", tt.Texts())
	}
}

func TestQAAnswersWithCitationsThatOpenTheBlock(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		call(agent.ToolGrep, `{"pattern":"延期"}`),
		func(req agent.Request) agent.Message {
			last := req.Messages[len(req.Messages)-1]
			if !strings.Contains(last.Text, "计划.md:5:决定延期发布") {
				t.Errorf("grep result: %q", last.Text)
			}
			return agent.Message{Role: agent.Assistant, Calls: []agent.ToolCall{{ID: "r", Name: agent.ToolRead, Args: `{"path":"计划.md","start_line":1,"end_line":7}`}}}
		},
		reply("**延期**是因为支付联调 [计划.md#L5]。\n\n- 阻塞：证书 [计划.md#L7]"),
	}}
	a, tt := qaApp(t, model)
	os.WriteFile(filepath.Join(root(a), "计划.md"), []byte("# 计划\n\n## 决定\n\n决定延期发布，原因是支付联调。\n\n证书还没下来。\n"), 0o644)
	a.qa.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Type("为什么延期？")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if sys := model.reqs[0].System; !strings.Contains(sys, `space "Personal"`) || !strings.Contains(sys, "计划.md") {
		t.Fatalf("system prompt:\n%s", sys)
	}
	qaSettle(t, tt, a.qa.qaSession(a.state.ActiveSpace().ID).turns[0])
	for _, want := range []string{
		"为什么延期？",
		"查找了 2 步 · 阅读 1 处",
		"延期",
		"是因为支付联调",
		"2 个来源",
	} {
		if !tt.HasText(want) {
			t.Errorf("no %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("第 5 行") {
		t.Fatalf("sources list should start collapsed: %q", tt.Texts())
	}
	if err := tt.Click("2 个来源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(240 * time.Millisecond)
	tt.Frame()
	for _, want := range []string{
		"计划 · 决定",
		"第 5 行",
		"第 7 行",
	} {
		if !tt.HasText(want) {
			t.Errorf("no %q in expanded sources: %q", want, tt.Texts())
		}
	}
	if a.qa.input != "" || len(a.qa.qaSession(a.state.ActiveSpace().ID).history) != 6 {
		t.Fatalf("history %+v", a.qa.qaSession(a.state.ActiveSpace().ID).history)
	}
	// The steps unfold to show clean structured labels and line ranges.
	tt.Click("查找了 2 步 · 阅读 1 处")
	tt.Frame()
	tt.Advance(220 * time.Millisecond)
	for _, want := range []string{"搜索", "“延期”", "找到 1 处", "阅读", "计划", "第 1–7 行", "已读 7 行"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in steps: %q", want, tt.Texts())
		}
	}
	// Clicking a search step unfolds its matched passage preview.
	tt.Click("搜索 “延期”")
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	if !tt.HasText("决定延期发布，原因是支付联调。") {
		t.Fatalf("search step preview did not unfold: %q", tt.Texts())
	}
	// A source row opens the document at the cited block.
	if err := tt.Click("2 计划 · 决定 第 7 行"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(slide)
	if a.doc == nil || a.doc.rel != "计划.md" {
		t.Fatalf("open: %+v", a.doc)
	}
	if got := a.doc.ed.Doc.Blocks[a.doc.ed.CaretBlock()].Text(); got != "证书还没下来。" {
		t.Fatalf("caret in %q", got)
	}
	// A follow-up sees the conversation so far.
	model.steps = append(model.steps, reply("好的"))
	a.qa.input = "谢谢"
	a.qa.askQA()
	if n := len(model.reqs[3].Messages); n != 7 || model.reqs[3].Messages[6].Text != "谢谢" {
		t.Fatalf("follow-up sent %d messages", n)
	}
}

func TestQASearchToolStepJumpAndLiveToggle(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		call(agent.ToolSearch, `{"query":"证书 风险"}`),
		call(agent.ToolRead, `{"path":"项目/发布计划.md","start_line":1,"end_line":7}`),
		reply("风险在安全证书 [项目/发布计划.md#L5]，同步见周报 [项目/周报.md#L3]。"),
	}}
	a, tt := qaApp(t, model)
	os.MkdirAll(filepath.Join(root(a), "项目"), 0o755)
	os.WriteFile(filepath.Join(root(a), "项目/发布计划.md"), []byte("# 发布计划\n\n## 风险\n\n安全证书审批延迟。\n"), 0o644)
	os.WriteFile(filepath.Join(root(a), "项目/周报.md"), []byte("# 周报\n\n已同步证书进度。\n"), 0o644)

	a.qa.toggleQA()
	tt.Frame()
	tt.Advance(slide)

	// Verify live step toggle works even while turn is running!
	sp := a.state.ActiveSpace().ID
	s := a.qa.qaSession(sp)
	liveTurn := &qaTurn{
		question: "正在跑的时候能折叠步骤吗？",
		running:  true,
		steps: []*qaStep{
			{call: agent.ToolCall{ID: "1", Name: agent.ToolSearch, Args: `{"query":"证书"}`}, done: true, result: "1 relevant passages in 1 documents.\n[1] 项目/发布计划.md (发布计划 · § 风险, L5)\n     5→安全证书审批延迟。\n"},
		},
	}
	s.turns = []*qaTurn{liveTurn}
	s.cancel = func() {}
	tt.Frame()
	if !tt.HasText("召回 1 段") {
		t.Fatalf("live timeline should start open: %q", tt.Texts())
	}
	tt.Click("查找了 1 步 · 阅读 0 处")
	tt.Frame()
	tt.Advance(220 * time.Millisecond)
	if liveTurn.stepsOpen() {
		t.Fatal("clicking header while running did not collapse timeline")
	}

	// Reset and run full scripted search + multi-document grouped citations.
	a.qa.clearQA()
	tt.Frame()
	a.qa.input = "证书有什么风险？"
	a.qa.askQA()
	qaSettle(t, tt, a.qa.qaSession(sp).turns[0])
	if !tt.HasText("2 个来源") {
		t.Fatalf("missing collapsed sources toggle: %q", tt.Texts())
	}
	if err := tt.Click("2 个来源"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(240 * time.Millisecond)
	tt.Frame()
	for _, want := range []string{"发布计划 · 风险", "项目", "第 5 行", "周报", "第 3 行"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in expanded sources: %q", want, tt.Texts())
		}
	}
	// Unfold timeline and click the read step directly to open 发布计划.md.
	tt.Click("查找了 2 步 · 阅读 1 处")
	tt.Frame()
	tt.Advance(220 * time.Millisecond)
	tt.Click("阅读 发布计划")
	tt.Frame()
	tt.Advance(slide)
	if a.doc == nil || a.doc.rel != "项目/发布计划.md" {
		t.Fatalf("clicking read step did not open document: %+v", a.doc)
	}
}

func TestQAReadsUnsavedEditsAndStaysInItsSpace(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		call(agent.ToolGrep, `{"pattern":"未保存的想法"}`),
		call(agent.ToolRead, `{"path":"../outside.md"}`),
		reply("找到了"),
	}}
	a, tt := qaApp(t, model)
	os.WriteFile(filepath.Join(filepath.Dir(root(a)), "outside.md"), []byte("外面"), 0o644)
	tt.Frame()
	focusDocument(a, tt)
	tt.Key(ui.Cmd, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Type("未保存的想法")
	tt.Frame()
	if !a.doc.dirty() {
		t.Fatal("the edit was saved already")
	}
	a.qa.input = "我刚写了什么？"
	a.qa.askQA()
	h := a.qa.qaSession(a.state.ActiveSpace().ID).history
	if !strings.Contains(h[2].Text, a.doc.rel+":") {
		t.Fatalf("unsaved text not searched: %q", h[2].Text)
	}
	if !h[4].IsError {
		t.Fatalf("read outside the space: %q", h[4].Text)
	}
}

func TestQAStopAndRetry(t *testing.T) {
	model := &scripted{}
	a, tt := qaApp(t, model)
	a.qa.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	sp := a.state.ActiveSpace().ID
	s := a.qa.qaSession(sp)
	// A run stopped partway keeps what it said and closes the turn.
	turn := &qaTurn{question: "q", running: true, answer: "一半"}
	s.turns = append(s.turns, turn)
	s.history = append(s.history, agent.Message{Role: agent.User, Text: "q"})
	_, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	tt.Frame()
	if !tt.HasText("一半") {
		t.Fatalf("partial answer: %q", tt.Texts())
	}
	a.qa.finishQA(s, turn, []agent.Message{{Role: agent.Assistant, Calls: []agent.ToolCall{{ID: "x"}}}, {Role: agent.ToolRole, CallID: "x"}}, context.Canceled)
	tt.Frame()
	last := s.history[len(s.history)-1]
	if !turn.stopped || s.running() || !tt.HasText("已停止") || last.Role != agent.Assistant || !strings.Contains(last.Text, "一半") {
		t.Fatalf("stopped: %+v %+v", turn, last)
	}
	// A failure offers to try again, which replaces the turn.
	model.steps = []func(agent.Request) agent.Message{reply("第二次成功")}
	failing := &qaTurn{question: "再问", start: len(s.history), running: true}
	s.turns = append(s.turns, failing)
	s.history = append(s.history, agent.Message{Role: agent.User, Text: "再问"})
	a.qa.finishQA(s, failing, nil, &agent.APIError{Status: 500, Message: "overloaded"})
	tt.Frame()
	if !tt.HasText("重试") || !tt.HasText("模型服务出错（500）：overloaded") {
		t.Fatalf("error: %q", tt.Texts())
	}
	tt.Click("重试")
	tt.Frame()
	qaSettle(t, tt, s.turns[1])
	if len(s.turns) != 2 || s.turns[1].answer != "第二次成功" || s.turns[1].err != nil || !tt.HasText("第二次成功") {
		t.Fatalf("retry: %+v", s.turns)
	}
	if len(s.history) != 6 || s.history[4].Text != "再问" || s.history[5].Text != "第二次成功" {
		t.Fatalf("retry history %+v", s.history)
	}
	// A new conversation starts empty.
	tt.Click("新对话")
	tt.Frame()
	if len(a.qa.qaSession(sp).turns) != 0 || !tt.HasText("向「Personal」提问") {
		t.Fatalf("new: %q", tt.Texts())
	}
}

func TestQAKeyStoreErrorIsShown(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "")
	a.qa.secrets = failingSecrets{}
	a.qa.keyRead = false
	if err := a.qa.qaReady(); err == nil || !strings.Contains(err.Error(), "无法读取 API Key") {
		t.Fatalf("%v", err)
	}
}

func TestQAKeyRemovalFailureKeepsSavedKey(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "sk-test-1234")
	a.qa.secrets = failingSecrets{}
	a.openSettings(tabAI)
	tt.Frame()
	if err := tt.Click("供应商与连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("移除 API Key"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(a.qa.keys) != 1 || a.qa.keys[0] != "sk-test-1234" || !tt.HasText(maskAPIKey("sk-test-1234")) || !tt.HasText("无法移除 API Key：locked") {
		t.Fatalf("failed removal discarded the saved key: %q", tt.Texts())
	}
}

type failingSecrets struct{}

func (failingSecrets) Get(string) ([]byte, error) { return nil, errors.New("locked") }
func (failingSecrets) Set(string, []byte) error   { return errors.New("locked") }
func (failingSecrets) Delete(string) error        { return errors.New("locked") }

func TestQAPageMapsBlocks(t *testing.T) {
	a, _ := newTestApp(t)
	content, err := os.ReadFile(filepath.Join(root(a), "Welcome.md"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := QAPage("Welcome.md", string(content))
	if err != nil || len(p.Lines) == 0 || len(p.Spans) == 0 || p.Spans[0].Block == "" {
		t.Fatalf("%v %+v", err, p)
	}
	legacy, err := editor.ImportMarkdown("# Legacy\n\nBody").Encode()
	if err != nil {
		t.Fatal(err)
	}
	lp, err := QAPage("Legacy.arcmd", legacy)
	if err != nil || len(lp.Lines) == 0 || len(lp.Spans) == 0 || lp.Spans[0].Block == "" {
		t.Fatalf("%v %+v", err, lp)
	}
	if _, err := QAPage("bad.arcmd", "{"); err == nil {
		t.Fatal("a broken document rendered")
	}
}

// The model list comes when asked for, with the address and key set (a
// key typed but not saved is saved first), and a name can still be typed.
