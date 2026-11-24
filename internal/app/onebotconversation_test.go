package app

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/egoist/mygo/ui"
)

func TestQAHorizontalRuleStaysHairline(t *testing.T) {
	a, tt := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{
		reply("第一部分结束。\n\n---\n\n## 第二部分标题\n\n继续内容。"),
	}})
	tt.SetSize(1280, 820)
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()

	a.qa.input = "测试分隔线粗细"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, a.qaSession(a.state.ActiveSpace().ID).turns[0])
	tt.Frame()

	r1, ok1 := tt.Find("第一部分结束。")
	r2, ok2 := tt.Find("第二部分标题")
	if !ok1 || !ok2 {
		t.Fatalf("missing answer blocks: ok1=%v ok2=%v texts=%q", ok1, ok2, tt.Texts())
	}
	img := tt.Image()
	x := int(r1.X + r1.W/2)
	bg := color.NRGBAModel.Convert(img.At(x, int(r1.Y+r1.H)+2)).(color.NRGBA)
	darkRows := 0
	for y := int(r1.Y+r1.H) + 1; y < int(r2.Y); y++ {
		px := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
		dr := int(bg.R) - int(px.R)
		dg := int(bg.G) - int(px.G)
		db := int(bg.B) - int(px.B)
		if dr < 0 {
			dr = -dr
		}
		if dg < 0 {
			dg = -dg
		}
		if db < 0 {
			db = -db
		}
		if dr+dg+db > 15 {
			darkRows++
		}
	}
	if darkRows < 1 || darkRows > 2 {
		t.Fatalf("horizontal rule rendered with %d scanlines between %+v and %+v, want 1-2px hairline", darkRows, r1, r2)
	}
}

func TestOneBotCopyAnswerCodeAndPrompt(t *testing.T) {
	answer := "主要结论如下 [Alpha.md#L3], [Beta.md#L5]：\n\n```go\nfmt.Println(\"ok\")\n```\n\n结束。"
	a, tt := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{
		reply(answer),
	}})
	os.WriteFile(filepath.Join(root(a), "Alpha.md"), []byte("# Alpha\n\nAlpha body\n"), 0o644)
	os.WriteFile(filepath.Join(root(a), "Beta.md"), []byte("# Beta\n\n\n\nBeta body\n"), 0o644)

	tt.SetSize(1280, 820)
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)

	a.qa.input = "请总结文档内容"
	a.askQA()
	tt.Frame()
	sp := a.state.ActiveSpace().ID
	qaSettle(t, tt, a.qaSession(sp).turns[0])
	tt.Frame()

	// Copy prompt button.
	if err := tt.Click("复制问题"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "请总结文档内容" {
		t.Fatalf("copied prompt = %q", got)
	}

	// Copy code button.
	if err := tt.Click("复制代码"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "fmt.Println(\"ok\")" {
		t.Fatalf("copied code = %q", got)
	}
	if !tt.HasText("已复制") {
		t.Fatalf("expected transient copied feedback: %q", tt.Texts())
	}
	tt.Advance(qaCopiedDuration + 50*time.Millisecond)
	tt.Frame()
	if tt.HasText("已复制") {
		t.Fatalf("transient copied feedback did not expire: %q", tt.Texts())
	}

	// Copy answer strips internal citation anchors cleanly while preserving Markdown code.
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	got := tt.Clipboard()
	if strings.Contains(got, "#L") || !strings.Contains(got, "主要结论如下：") || !strings.Contains(got, "```go\nfmt.Println(\"ok\")\n```") {
		t.Fatalf("cleaned answer copy = %q", got)
	}

	// Plain text copy via menu.
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制纯文本"); err != nil {
		t.Fatalf("%v; menu=%q", err, tt.Menu())
	}
	plain := tt.Clipboard()
	if strings.Contains(plain, "```") || strings.Contains(plain, "#L") {
		t.Fatalf("plain copy kept markdown or citation syntax: %q", plain)
	}

	// Full conversation export from header menu.
	if err := tt.Click("OneBot 菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("复制完整对话"); err != nil {
		t.Fatalf("%v; menu=%q", err, tt.Menu())
	}
	if exported := tt.Clipboard(); !strings.Contains(exported, "### 请总结文档内容") || !strings.Contains(exported, "主要结论如下：") {
		t.Fatalf("exported conversation = %q", exported)
	}
}

func TestOneBotRegenerateEditAndContinue(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		reply("第一轮旧回复"),
		reply("第二轮回复"),
		reply("第一轮重生成"),
		reply("编辑后重发回复"),
		reply("继续后的结尾。"),
	}}
	a, tt := qaApp(t, model)
	tt.SetSize(1280, 820)
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	sp := a.state.ActiveSpace().ID
	s := a.qaSession(sp)

	a.qa.input = "第一个问题"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, s.turns[0])

	a.qa.input = "第二个问题"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, s.turns[1])
	tt.Frame()

	// Regenerating turn 1 truncates turn 2 and reruns turn 1 in place.
	if err := tt.Click("重新生成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	qaSettle(t, tt, s.turns[0])
	tt.Frame()
	if len(s.turns) != 1 || s.turns[0].answer != "第一轮重生成" || tt.HasText("第二轮回复") {
		t.Fatalf("regenerate turn 1 failed: turns=%d ans=%q texts=%q", len(s.turns), s.turns[0].answer, tt.Texts())
	}

	// Inline edit user prompt and resend.
	if err := tt.Click("编辑并重发"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.qa.editingText = "修改后的问题"
	if err := tt.Click("保存并发送"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	qaSettle(t, tt, s.turns[0])
	tt.Frame()
	if s.turns[0].question != "修改后的问题" || s.turns[0].answer != "编辑后重发回复" {
		t.Fatalf("edit and resend failed: q=%q a=%q", s.turns[0].question, s.turns[0].answer)
	}

	// Truncated response (finishReason == "length") exposes Continue button.
	s.turns[0].finishReason = "length"
	tt.Frame()
	if !tt.HasText("继续生成") {
		t.Fatalf("expected 继续生成 button when finishReason=length: %q", tt.Texts())
	}
	if err := tt.Click("继续生成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	qaSettle(t, tt, s.turns[1])
	tt.Frame()
	if len(s.turns) != 2 || s.turns[1].answer != "继续后的结尾。" {
		t.Fatalf("continue generation failed: turns=%d", len(s.turns))
	}
}

func TestOneBotClearContextUndoSlashAndPersistence(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		reply("旧主题回答"),
		reply("新主题回答"),
		reply("续接新主题"),
	}}
	a, tt := qaApp(t, model)
	tt.SetSize(1280, 820)
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	sp := a.state.ActiveSpace().ID
	s := a.qaSession(sp)

	a.qa.input = "旧主题问题"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, s.turns[0])
	tt.Frame()

	// Clear context from composer toolbar button preserves transcript above divider.
	if err := tt.Click("清除上下文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("以上上下文已清除 · 重新开始") || !tt.HasText("新上下文") || !tt.HasText("旧主题回答") {
		t.Fatalf("context break divider missing: %q", tt.Texts())
	}

	// Undo restores context when clicked before sending new turn.
	if err := tt.Click("撤销"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("以上上下文已清除 · 重新开始") || !tt.HasText("1 轮上下文") {
		t.Fatalf("undo did not restore context: %q", tt.Texts())
	}

	// /clear slash command triggers context clear.
	a.qa.input = "/clear"
	a.askQA()
	tt.Frame()
	if !s.turns[0].contextBreakAfter || !tt.HasText("以上上下文已清除 · 重新开始") {
		t.Fatal("/clear slash shortcut did not mark context break")
	}

	// Next turn after clear must only send turn 2 to model, without turn 1 history.
	a.qa.input = "新主题问题"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, s.turns[1])
	tt.Frame()
	if len(model.reqs) != 2 || len(model.reqs[1].Messages) != 1 || model.reqs[1].Messages[0].Text != "新主题问题" {
		t.Fatalf("post-clear model messages leaked old context: %+v", model.reqs[1].Messages)
	}
	if !tt.HasText("1 轮上下文") {
		t.Fatalf("expected composer counter to report 1-turn active context: %q", tt.Texts())
	}

	// Reopen persisted conversation: contextStart & contextBreak survive reload.
	convID := s.id
	a.openOneBotConversation(sp, convID) // no-op when already active; force reload via fresh session.
	a.qa.sessions[sp] = &qaSession{spaceID: sp, list: ui.ListState{FollowEnd: true}}
	a.openOneBotConversation(sp, convID)
	reloaded := a.qaSession(sp)
	if len(reloaded.turns) != 2 || !reloaded.turns[0].contextBreakAfter || reloaded.contextStart != 2 {
		t.Fatalf("persisted context break not restored: turns=%d ctxStart=%d", len(reloaded.turns), reloaded.contextStart)
	}

	// Next question on reloaded session also respects restored contextStart.
	a.qa.input = "续问新主题"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, reloaded.turns[2])
	for _, msg := range model.reqs[2].Messages {
		if strings.Contains(msg.Text, "旧主题") {
			t.Fatalf("reloaded session leaked cleared turn into request: %+v", model.reqs[2].Messages)
		}
	}

	// /new slash command starts fresh empty session.
	a.qa.input = "/new"
	a.askQA()
	tt.Frame()
	if len(a.qaSession(sp).turns) != 0 || !tt.HasText("向「Personal」提问") {
		t.Fatalf("/new slash command did not reset session: %q", tt.Texts())
	}
}

func TestOneBotForkDeleteAndInsertToDocument(t *testing.T) {
	model := &scripted{steps: []func(agent.Request) agent.Message{
		reply("分支点回答 [Welcome.md#L1]"),
		reply("第二轮会被截断的回答"),
	}}
	a, tt := qaApp(t, model)
	a.doc.ed = editor.New("# 目标文档\n\n")
	a.doc.ed.SetSelection(len("# 目标文档\n\n"), len("# 目标文档\n\n"))
	tt.SetSize(1280, 820)
	a.toggleQA()
	tt.Frame()
	tt.Advance(slide)
	sp := a.state.ActiveSpace().ID

	a.qa.input = "第一问"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, a.qaSession(sp).turns[0])
	origID := a.qaSession(sp).id

	a.qa.input = "第二问"
	a.askQA()
	tt.Frame()
	qaSettle(t, tt, a.qaSession(sp).turns[1])
	tt.Frame()

	// Insert cleaned turn 1 answer into writable active document.
	if err := tt.Click("插入到当前文档"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	plain := a.doc.ed.Doc.PlainText()
	if !strings.Contains(plain, "分支点回答") || strings.Contains(plain, "#L1") {
		t.Fatalf("insert to doc = %q", plain)
	}

	// Fork from turn 1 creates a new independent session with turns[:1].
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("从此轮新建分支对话"); err != nil {
		t.Fatalf("%v; menu=%q", err, tt.Menu())
	}
	tt.Frame()
	fork := a.qaSession(sp)
	if fork.id == "" || fork.id == origID || len(fork.turns) != 1 || tt.HasText("第二轮会被截断的回答") {
		t.Fatalf("fork failed: forkID=%q origID=%q turns=%d", fork.id, origID, len(fork.turns))
	}

	// Delete turn 1 resets session back to empty state.
	if err := tt.Click("更多操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除此轮及后续"); err != nil {
		t.Fatalf("%v; menu=%q", err, tt.Menu())
	}
	tt.Frame()
	if len(a.qaSession(sp).turns) != 0 || !tt.HasText("向「Personal」提问") {
		t.Fatalf("delete turn 1 did not reset session: turns=%d", len(a.qaSession(sp).turns))
	}
}
