package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func runEditTool(t *testing.T, w *EditWorkspace, name, args string) (string, error) {
	t.Helper()
	for _, tool := range w.Tools(nil) {
		if tool.Spec.Name == name {
			return tool.Run(context.Background(), json.RawMessage(args))
		}
	}
	t.Fatal("missing tool", name)
	return "", nil
}

func TestOpenCodeToolsStageExactEditsAndReadUpdatedContent(t *testing.T) {
	w, err := NewEditWorkspace([]EditFile{{"note.md", "前😀 old\r\nkeep\r\n"}}, []string{"new.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runEditTool(t, w, "edit", `{"path":"note.md","oldString":"前😀 old","newString":"前😀 new"}`); err != nil {
		t.Fatal(err)
	}
	text, err := runEditTool(t, w, "read", `{"path":"note.md","offset":1,"limit":1}`)
	if err != nil || !strings.Contains(text, "1: 前😀 new") || strings.Contains(text, "2: keep") {
		t.Fatalf("read %q %v", text, err)
	}
	if _, err = runEditTool(t, w, "write", `{"path":"new.md","content":"created"}`); err != nil {
		t.Fatal(err)
	}
	changes := w.Changes()
	if len(changes) != 2 || changes[1].After != "前😀 new\r\nkeep\r\n" || changes[1].Before != "前😀 old\r\nkeep\r\n" {
		t.Fatalf("changes %+v", changes)
	}
}

func TestOpenCodeToolsRejectOutOfScopeAndPreparePatchAtomically(t *testing.T) {
	w, _ := NewEditWorkspace([]EditFile{{"a.md", "old\n"}, {"b.md", "second\n"}}, nil)
	for _, path := range []string{"../secret.md", "other.md", "C:/secret.md"} {
		args, _ := json.Marshal(map[string]string{"path": path, "content": "bad"})
		if _, err := runEditTool(t, w, "write", string(args)); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	patch := "*** Begin Patch\n*** Update File: a.md\n@@\n-old\n+new\n*** Update File: b.md\n@@\n-missing\n+bad\n*** End Patch"
	if err := w.Patch(patch); err == nil || len(w.Changes()) != 0 {
		t.Fatal("partial invalid patch was staged")
	}
	patch = strings.ReplaceAll(patch, "-missing\n+bad", "-second\n+good")
	if err := w.Patch(patch); err != nil || len(w.Changes()) != 2 {
		t.Fatalf("valid multi-file patch: %v", err)
	}
}

func TestGenerateFileEditUsesOpenCodeToolsAndRejectsIncompleteAnswer(t *testing.T) {
	for _, finish := range []string{"stop", "length"} {
		t.Run(finish, func(t *testing.T) {
			w, _ := NewEditWorkspace([]EditFile{{"a.md", "old"}}, nil)
			steps := 0
			provider := editProviderFunc(func(ctx context.Context, r Request, d func(Delta)) (Response, error) {
				steps++
				for _, tool := range r.Tools {
					if tool.Name == "propose_edits" || tool.Name == "edit_targets" {
						t.Fatal("custom edit protocol exposed")
					}
				}
				if steps == 1 {
					return Response{Message: Message{Calls: []ToolCall{{ID: "e", Name: "edit", Args: `{"path":"a.md","oldString":"old","newString":"new"}`}}}, FinishReason: "tool_calls"}, nil
				}
				return Response{Message: Message{Text: "Ready"}, FinishReason: finish}, nil
			})
			draft, err := GenerateFileEdit(context.Background(), provider, "rewrite", nil, w, nil, true, func(Delta) {})
			if finish == "stop" {
				if err != nil || len(draft.Changes) != 1 || draft.Changes[0].After != "new" || steps != 2 {
					t.Fatalf("draft %+v %v", draft, err)
				}
			} else if !errors.Is(err, ErrIncomplete) || len(draft.Changes) != 0 {
				t.Fatal("truncated response accepted")
			}
		})
	}
}

func TestGenerateFileEditWithoutToolsUsesUpstreamPatchGrammar(t *testing.T) {
	w, _ := NewEditWorkspace([]EditFile{{"a.md", "old\n"}}, nil)
	provider := editProviderFunc(func(_ context.Context, r Request, _ func(Delta)) (Response, error) {
		if len(r.Tools) != 0 || !strings.Contains(r.Messages[0].Text, "old") {
			t.Fatal("missing source")
		}
		return Response{Message: Message{Text: "*** Begin Patch\n*** Update File: a.md\n@@\n-old\n+new\n*** End Patch"}, FinishReason: "stop"}, nil
	})
	draft, err := GenerateFileEdit(context.Background(), provider, "rewrite", nil, w, nil, false, func(Delta) {})
	if err != nil || len(draft.Changes) != 1 || draft.Changes[0].After != "new\n" {
		t.Fatalf("%+v %v", draft, err)
	}
}

func TestOpenCodeToolMutationsFollowModelOrder(t *testing.T) {
	w, _ := NewEditWorkspace([]EditFile{{"a.md", "old"}}, nil)
	p := editProviderFunc(func(_ context.Context, r Request, _ func(Delta)) (Response, error) {
		if len(r.Messages) == 1 {
			return Response{Message: Message{Calls: []ToolCall{
				{ID: "1", Name: "edit", Args: `{"path":"a.md","oldString":"old","newString":"middle"}`},
				{ID: "2", Name: "edit", Args: `{"path":"a.md","oldString":"middle","newString":"new"}`},
				{ID: "3", Name: "read", Args: `{"path":"a.md"}`},
			}}, FinishReason: "tool_calls"}, nil
		}
		if !strings.Contains(r.Messages[len(r.Messages)-1].Text, "1: new") {
			t.Fatal("read ran before preceding mutations")
		}
		return Response{Message: Message{Text: "done"}, FinishReason: "stop"}, nil
	})
	draft, err := GenerateFileEdit(context.Background(), p, "edit", nil, w, nil, true, nil)
	if err != nil || len(draft.Changes) != 1 || draft.Changes[0].After != "new" {
		t.Fatalf("ordered edit: %+v %v", draft, err)
	}
}

type editProviderFunc func(context.Context, Request, func(Delta)) (Response, error)

func (f editProviderFunc) Complete(c context.Context, r Request, d func(Delta)) (Response, error) {
	return f(c, r, d)
}
