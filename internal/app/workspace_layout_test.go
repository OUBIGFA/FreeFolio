package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/egoist/mygo/ui"
)

func TestOneBotDraftFitsItsContents(t *testing.T) {
	app, tester := qaApp(t, &scripted{steps: []func(agent.Request) agent.Message{reply("收到")}})
	app.toggleQA()
	settle(tester)
	field := func() ui.Rect {
		t.Helper()
		tester.Frame()
		bounds, found := tester.Find("问题")
		if !found {
			t.Fatal("missing draft")
		}
		return bounds
	}
	empty := field()
	if empty.H > 32 {
		t.Errorf("empty draft reserves %.0f pixels", empty.H)
	}
	tester.Type("第一行")
	tester.Key(ui.Shift, ui.KeyEnter)
	tester.Type("第二行")
	if two := field(); app.qa.input != "第一行\n第二行" || two.H < empty.H+10 {
		t.Fatalf("multiline draft %q: one=%v two=%v", app.qa.input, empty, two)
	}
	app.qa.input = strings.Repeat("较长的单行草稿", 16)
	if wrapped := field(); wrapped.H <= empty.H {
		t.Fatalf("wrapped draft did not grow: %v", wrapped)
	}
	app.qa.input = strings.Repeat("换行\n", 40)
	if long := field(); long.H <= empty.H || long.H > 160 {
		t.Fatalf("long draft height %v", long)
	}
	app.qa.input = "发出这条消息"
	field()
	tester.Key(0, ui.KeyEnter)
	if sent := field(); len(app.qaSession(app.state.ActiveSpace().ID).turns) != 1 || app.qa.input != "" || sent.H != empty.H || sent.Y < 615 {
		t.Fatalf("send did not dock an empty draft: %v, draft %q", sent, app.qa.input)
	}
}

func TestOneBotWelcomeStaysWithDraft(t *testing.T) {
	app, tester := newTestApp(t)
	setupOneBot(app, "key")
	app.toggleQA()
	settle(tester)
	position := func() float32 {
		t.Helper()
		tester.Frame()
		before, foundHint := tester.Find("提问时，读到的文档片段会发送给你设置的模型服务")
		draft, foundDraft := tester.Find("问题")
		if !foundHint || !foundDraft {
			t.Fatalf("missing welcome or draft: %v, %v", before, draft)
		}
		return draft.Y - before.Y - before.H
	}
	gap := position()
	if gap < 0 || gap > 42 {
		t.Errorf("welcome to draft gap = %.0f", gap)
	}
	app.qa.attachments = []oneBotAttachmentItem{{Part: agent.ContentPart{Name: "参考.md"}, Kind: "text"}}
	if attached := position(); attached < gap+15 {
		t.Fatalf("attachment row did not expand: %.0f to %.0f", gap, attached)
	}
	if err := tester.Click("移除附件：参考.md"); err != nil {
		t.Fatal(err)
	}
	if after := position(); after != gap {
		t.Fatalf("empty attachment row still takes space: %.0f, want %.0f", after, gap)
	}
}

func TestNarrowSettingsUseFullWidthForContent(t *testing.T) {
	app, tester := newTestApp(t)
	tester.SetSize(640, 420)
	app.openSettings(tabGeneral)
	settle(tester)
	if err := tester.Click("选择设置页面"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("写作"); err != nil {
		t.Fatalf("%v: %v", err, tester.Texts())
	}
	tester.Frame()
	label, found := tester.Find("打字机滚动")
	if app.settingsTab != tabWriting || !found || label.X > 95 {
		t.Fatalf("page %q still reserves a navigation rail: %v", app.settingsTab, label)
	}
	tester.SetSize(1120, 760)
	settle(tester)
	if _, found := tester.Find("选择设置页面"); found || !tester.HasText("快捷键") || app.settingsTab != tabWriting {
		t.Fatal("wide navigation did not return to the same page")
	}
}

func TestWorkspaceHomeResumesOnlyItsOwnDocuments(t *testing.T) {
	app, tester := newTestApp(t)
	app.open("Journal/Ideas.md")
	app.open("Welcome.md")
	app.closeShownDoc()
	other := app.state.Add("Other", t.TempDir())
	app.history = append(app.history, place{space: other.ID, rel: "Private.md"}, place{space: app.state.ActiveSpace().ID, rel: "Gone.md"})
	settle(tester)
	if !tester.HasText("最近打开") || tester.HasText("继续打开：Private.md") || tester.HasText("继续打开：Gone.md") {
		t.Fatal("home must show only recent documents of this space")
	}
	if err := tester.Click("继续打开：Journal/Ideas.md"); err != nil {
		t.Fatal(err)
	}
	if app.doc == nil || app.doc.rel != "Journal/Ideas.md" {
		t.Fatal("recent document was not opened")
	}
}

func TestWorkspaceSearchExposesCurrentResult(t *testing.T) {
	app, tester := newTestApp(t)
	app.openPalette()
	app.palette.query = "Welcome"
	settle(tester)
	results := app.results()
	if len(results) == 0 {
		t.Fatal("expected a document result")
	}
	if _, found := tester.AccessByRole(ui.RoleListBox, "搜索结果"); !found {
		t.Fatal("search results are not exposed as a list")
	}
	if option, found := tester.AccessByRole(ui.RoleOption, results[0].title); !found || !option.Selected {
		t.Fatal("current result has no option role")
	}
}

func TestOneBotAttachmentListLeavesRoomForDraft(t *testing.T) {
	app, tester := newTestApp(t)
	setupOneBot(app, "key")
	tester.SetSize(640, 420)
	app.prefs.OneBotWidth = 280
	app.toggleQA()
	for index := range oneBotMaxAttachments {
		app.qa.attachments = append(app.qa.attachments, oneBotAttachmentItem{Part: agent.ContentPart{Name: fmt.Sprintf("一个较长的附件名称-%02d.md", index)}})
	}
	settle(tester)
	field, found := tester.Find("问题")
	send, canSend := tester.Find("发送 (Enter)")
	if !found || field.H < 20 || !canSend || send.H < 24 || send.Y+send.H > 420 {
		t.Fatalf("attachments pushed the draft or send button off-screen: field=%v send=%v", field, send)
	}
}
