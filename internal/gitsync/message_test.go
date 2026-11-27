package gitsync

import (
	"context"
	"errors"
	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"strings"
	"testing"
)

type messageProvider func(agent.Request) (agent.Response, error)

func (f messageProvider) Complete(_ context.Context, r agent.Request, _ func(agent.Delta)) (agent.Response, error) {
	return f(r)
}
func TestCommitMessageUsesBoundedDiffWithoutTools(t *testing.T) {
	p := messageProvider(func(r agent.Request) (agent.Response, error) {
		if len(r.Tools) != 0 || len(r.Messages) != 1 || !strings.Contains(r.Messages[0].Text, "note.md") || len(r.Messages[0].Text) > 100000 {
			t.Fatal(r)
		}
		return agent.Response{Message: agent.Message{Text: "更新项目笔记"}}, nil
	})
	msg, e := GenerateMessage(context.Background(), p, "note.md\n"+strings.Repeat("change\n", 30000), "中文", "简短")
	if e != nil || msg != "更新项目笔记" {
		t.Fatal(msg, e)
	}
	p = messageProvider(func(agent.Request) (agent.Response, error) { return agent.Response{}, errors.New("offline") })
	if _, e = GenerateMessage(context.Background(), p, "note.md", "中文", ""); e == nil {
		t.Fatal("provider failure hidden")
	}
}
