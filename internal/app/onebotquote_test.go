package app

import (
	"context"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	"github.com/OUBIGFA/FreeMD/internal/editor"
)

// "添加到 OneBot" quotes the selection in OneBot's input as obsidian-yolo
// does: a badge until sent, then <user_selected_content> with source line
// numbers before the question, the question itself shown unchanged.
func TestSelectionBarAddToOneBotQuotesSelection(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "key")
	a.qa.sync = true
	d := a.doc
	d.ed.ReadOnly = false
	d.ed.Engine.ReplaceDocument(editor.ImportMarkdown("# Title\n\nhello world\n\nkeep this\n"))
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	var sent string
	a.qa.provider = func(agent.Settings) agent.Provider {
		return oneBotProviderFunc(func(ctx context.Context, r agent.Request, delta func(agent.Delta)) (agent.Response, error) {
			for _, m := range r.Messages {
				if m.Role == agent.User {
					sent = m.Text
				}
			}
			return agent.Response{Message: agent.Message{Text: "ok"}, FinishReason: "stop"}, nil
		})
	}
	d.ed.RequestFocus = true
	tt.Frame()
	start := d.ed.LeafAtIndex(1).Start
	d.ed.SetSelection(start, start+5)
	tt.Frame()
	if err := tt.Click("OneBot 修改选区"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到 OneBot"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	settle(tt)
	if !a.qa.open || len(a.qa.quotes) != 1 {
		t.Fatalf("OneBot open=%v quotes=%+v", a.qa.open, a.qa.quotes)
	}
	q := a.qa.quotes[0]
	if q.Path != d.rel || q.Text != "hello" || q.From != 3 || q.To != 3 {
		t.Fatalf("quote %+v", q)
	}
	name := q.Path[strings.LastIndex(q.Path, "/")+1:]
	if !tt.HasText("引用：" + name) {
		t.Fatal("quote badge missing", tt.Texts())
	}
	// Adding the same selection again keeps one badge.
	a.addSelectionToOneBot(d)
	if len(a.qa.quotes) != 1 {
		t.Fatal("duplicate quote")
	}
	a.qa.input = "这句话什么意思？"
	a.askQA()
	settle(tt)
	want := `<user_selected_content path="` + d.rel + `" startLine="3" endLine="3">` + "\n```" + d.rel + "\n3|hello\n```\n</user_selected_content>\n\n这句话什么意思？"
	if sent != want {
		t.Fatalf("sent:\n%s\nwant:\n%s", sent, want)
	}
	if len(a.qa.quotes) != 0 {
		t.Fatal("quotes stay after sending")
	}
	turns := a.qaSession(a.state.ActiveSpace().ID).turns
	if last := turns[len(turns)-1]; last.question != "这句话什么意思？" || len(last.quotes) != 1 {
		t.Fatalf("turn %q %+v", last.question, last.quotes)
	}
	if !tt.HasText("引用：" + name) {
		t.Fatal("the sent question lost its quote badge")
	}
}

// The composer's model button names the provider with the model.
func TestOneBotComposerShowsProviderAndModel(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "key")
	a.qa.open = true
	settle(tt)
	if !tt.HasText("Test / test-model") {
		t.Fatal("model button lacks the provider", tt.Texts())
	}
}
