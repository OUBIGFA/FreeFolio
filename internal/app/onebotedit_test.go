package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
)

func TestOneBotFilePatchAllowsPartialReviewAndKeepsUnrelatedEdits(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	editTestTask(t, a)
	a.doc.ed.Engine.ReplaceDocument(editor.ImportMarkdown("first\n\nkeep\n\nlast\n"))
	if err := a.doc.save(); err != nil {
		t.Fatal(err)
	}
	task, err := a.qa.captureOneBotTask("document")
	if err != nil {
		t.Fatal(err)
	}
	w, _ := agent.NewEditWorkspace(oneBotEditFiles(task), nil)
	patch := "*** Begin Patch\n*** Update File: " + a.doc.rel + "\n@@\n-first\n+FIRST\n@@\n-last\n+LAST\n*** End Patch"
	if err = w.Patch(patch); err != nil {
		t.Fatal(err)
	}
	// A separate paragraph can be changed after capture without invalidating
	// the two actual patch targets.
	keep := a.doc.ed.LeafAtIndex(1)
	a.doc.ed.ReplaceLiteral(keep.Start, keep.End, "manual")
	candidate, err := a.qa.prepareOneBotFileCandidate(task, agent.FileDraft{Changes: w.Changes()})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.Changes) != 2 {
		t.Fatalf("review groups: %d", len(candidate.Changes))
	}
	task.Candidates = []*oneBotCandidate{candidate}
	if err = a.qa.applyOneBotCandidate(task, 0, 0); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.Doc.PlainText() != "FIRST\nmanual\nlast" {
		t.Fatal(a.doc.ed.Doc.PlainText())
	}
	if err = a.qa.applyOneBotCandidate(task, 0, 1); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.Doc.PlainText() != "FIRST\nmanual\nLAST" {
		t.Fatal(a.doc.ed.Doc.PlainText())
	}
	a.qa.revertOneBotCandidate(task, candidate)
	if a.doc.ed.Doc.PlainText() != "first\nmanual\nlast" {
		t.Fatal("task recovery lost unrelated edit", a.doc.ed.Doc.PlainText())
	}
}

func TestOneBotFollowUpKeepsEarlierDraftAndUsesNewWholeDocument(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	editTestTask(t, a)
	task, err := a.qa.captureOneBotTask("document")
	if err != nil {
		t.Fatal(err)
	}
	draft := selectionFileDraft(task, strings.Replace(task.Targets[0].Markdown, "hello", "hi", 1))
	candidate, err := a.qa.prepareOneBotFileCandidate(task, draft)
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{candidate}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err != nil {
		t.Fatal(err)
	}
	next, err := a.qa.continueOneBotTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Targets) != 1 || !next.Targets[0].Whole || !strings.Contains(next.Targets[0].Markdown, "hi world") || strings.Contains(task.Targets[0].Markdown, "hi world") {
		t.Fatal("follow-up did not preserve independent contexts")
	}
}

func TestOneBotFileToolAndCitationUseSameMarkdownLines(t *testing.T) {
	source := "---\ncustom: value\n---\n\n# Title\n- first\n- second\n\n**last**\n"
	d := editor.ImportMarkdown(source)
	page := pageOfDocument("note.md", d)
	if strings.Join(page.Lines, "\n")+"\n" != source {
		t.Fatal("citation view differs from file tool source")
	}
	span, ok := page.Locate(9)
	if !ok || span.Index >= len(d.Blocks) || d.Blocks[span.Index].Text() != "last" {
		t.Fatalf("citation points at wrong block: %+v", span)
	}
}

func TestOneBotContinueAfterNewFileEditsCreatedDocument(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	task, err := a.qa.captureOneBotTask("new")
	if err != nil {
		t.Fatal(err)
	}
	task.NewPath = "Created.md"
	c, err := a.qa.prepareOneBotFileCandidate(task, agent.FileDraft{Changes: []agent.FileChange{{Path: task.NewPath, After: "created\n", Created: true}}})
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{c}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err != nil {
		t.Fatal(err)
	}
	next, err := a.qa.continueOneBotTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if next.Scope != "document" || next.NewPath != "" || len(next.Targets) != 1 || next.Targets[0].Path != "Created.md" {
		t.Fatalf("new file follow-up: %+v", next)
	}
}

func TestOneBotContinueAfterSectionEditKeepsSectionScope(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	editTestTask(t, a)
	a.doc.ed.Engine.ReplaceDocument(editor.ImportMarkdown("# One\n\nold\n\n# Two\n\nkeep\n"))
	a.doc.ed.SetSelection(5, 5)
	task, err := a.qa.captureOneBotTask("section")
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.qa.prepareOneBotFileCandidate(task, selectionFileDraft(task, strings.Replace(task.Targets[0].Markdown, "old", "new", 1)))
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{c}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err != nil {
		t.Fatal(err)
	}
	next, err := a.qa.continueOneBotTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(next.Targets[0].Markdown, "new") || strings.Contains(next.Targets[0].Markdown, "keep") {
		t.Fatalf("section escaped: %+v", next.Targets)
	}
}

func TestOneBotClearedChatCannotApplyLateFileTools(t *testing.T) {
	fake := &scripted{}
	a, _ := qaApp(t, fake)
	editTestTask(t, a)
	a.prefs.AI.Direct = true
	args, _ := json.Marshal(map[string]string{"path": a.doc.rel, "oldString": "hello", "newString": "late"})
	fake.steps = []func(agent.Request) agent.Message{call("edit", string(args)), func(agent.Request) agent.Message { a.qa.clearQA(); return agent.Message{Text: "done"} }}
	a.qa.ask(a.state.ActiveSpace().ID, "请修改", nil)
	if a.doc.ed.Doc.PlainText() != "hello world\nkeep this" || len(a.qa.qaSession(a.state.ActiveSpace().ID).turns) != 0 {
		t.Fatal("cleared chat applied late result")
	}
}

func editTestTask(t *testing.T, a *App) *oneBotTask {
	t.Helper()
	d := a.doc
	d.ed.ReadOnly = false
	d.ed.Engine.ReplaceDocument(&model.Document{Format: "arcmd", Version: model.CurrentVersion, Blocks: []*model.Block{model.NewBlock("paragraph", "hello world"), model.NewBlock("paragraph", "keep this")}})
	if err := d.save(); err != nil {
		t.Fatal(err)
	}
	d.ed.SetSelection(0, 5)
	task, err := a.qa.captureOneBotTask("selection")
	if err != nil {
		t.Fatal(err)
	}
	return task
}

type oneBotProviderFunc func(context.Context, agent.Request, func(agent.Delta)) (agent.Response, error)

func (f oneBotProviderFunc) Complete(ctx context.Context, r agent.Request, d func(agent.Delta)) (agent.Response, error) {
	return f(ctx, r, d)
}

func TestOneBotGenerationDirectAndCandidates(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			a, _ := newTestApp(t)
			setupOneBot(a, "key")
			task := editTestTask(t, a)
			task.Direct = true
			a.qa.sync = true
			a.qa.provider = func(agent.Settings) agent.Provider {
				return oneBotProviderFunc(func(ctx context.Context, r agent.Request, delta func(agent.Delta)) (agent.Response, error) {
					if len(r.Messages) == 1 {
						data, _ := json.Marshal(map[string]string{"path": task.Targets[0].Path, "oldString": "hello", "newString": "hi"})
						return agent.Response{Message: agent.Message{Calls: []agent.ToolCall{{ID: "edit", Name: "edit", Args: string(data)}}}, FinishReason: "tool_calls"}, nil
					}
					delta(agent.Delta{Text: "已准备修改"})
					return agent.Response{Message: agent.Message{Text: "已准备修改"}, FinishReason: "stop"}, nil
				})
			}
			a.qa.runOneBotEdit(task, "shorten", count, false)
			if task.Running || len(task.Candidates) != count || !task.Candidates[0].Complete {
				t.Fatalf("generation %+v", task)
			}
			want := "hello world\nkeep this"
			if count == 1 {
				want = "hi world\nkeep this"
			}
			if a.doc.ed.Doc.PlainText() != want {
				t.Fatal("incorrect automatic application")
			}
		})
	}
}

func TestOneBotReopenedDraftDoesNotGuessDuplicateBlock(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	task := editTestTask(t, a)
	data, _ := json.Marshal(task)
	var loaded oneBotTask
	json.Unmarshal(data, &loaded)
	a.doc.ed.Engine.ReplaceDocument(&model.Document{Format: "arcmd", Version: model.CurrentVersion, Blocks: []*model.Block{model.NewBlock("paragraph", "hello world"), model.NewBlock("paragraph", "hello world")}})
	if _, err := a.qa.oneBotTargetDoc(&loaded, &loaded.Targets[0]); !errors.Is(err, model.ErrEditConflict) {
		t.Fatalf("ambiguous original matched: %v", err)
	}
}

func TestOneBotNewFileFailureDoesNotApplyOtherDocument(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	task := editTestTask(t, a)
	task.NewPath = "taken.md"
	os.WriteFile(filepath.Join(root(a), "taken.md"), []byte("keep"), 0600)
	c, err := a.qa.prepareOneBotFileCandidate(task, agent.FileDraft{Changes: []agent.FileChange{{Path: task.Targets[0].Path, Before: task.Targets[0].Markdown, After: "hi"}, {Path: "taken.md", After: "new", Created: true}}})
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{c}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err == nil || a.doc.ed.Doc.PlainText() != "hello world\nkeep this" {
		t.Fatal("known creation conflict applied another edit")
	}
}

func TestOneBotContinueUsesAppliedSelection(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	task := editTestTask(t, a)
	candidate, err := a.qa.prepareOneBotFileCandidate(task, selectionFileDraft(task, "hello again"))
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{candidate}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err != nil {
		t.Fatal(err)
	}
	next, err := a.qa.continueOneBotTask(task)
	if err != nil || len(next.Targets) != 1 || next.Targets[0].Target.Text != "hello again" || next.ID == task.ID {
		t.Fatalf("continuation: %+v %v", next, err)
	}
}

func TestOneBotChatPreparesDraftWithoutWriting(t *testing.T) {
	fake := &scripted{}
	a, _ := qaApp(t, fake)
	editTestTask(t, a)
	readArgs, _ := json.Marshal(map[string]any{"path": a.doc.rel, "offset": 1, "limit": 2000})
	editArgs, _ := json.Marshal(map[string]string{"path": a.doc.rel, "oldString": "hello", "newString": "changed"})
	fake.steps = []func(agent.Request) agent.Message{call("read", string(readArgs)), call("edit", string(editArgs)), reply("已准备修改，请审阅。")}
	before := a.doc.text()
	a.qa.ask(a.state.ActiveSpace().ID, "请修改第一段", nil)
	turn := a.qa.qaSession(a.state.ActiveSpace().ID).turns[0]
	if turn.edit == nil || a.doc.text() != before {
		t.Fatalf("chat did not prepare a safe draft: %+v", turn)
	}
	if err := a.qa.applyOneBotCandidate(turn.edit, 0, -1); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.Doc.PlainText() != "changed world\nkeep this" {
		t.Fatal("wrong edit result", a.doc.ed.Doc.PlainText())
	}
}

func TestOneBotReviewAppliesOnlyOnAcceptAndPersistsRecovery(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	task := editTestTask(t, a)
	proposal := selectionFileDraft(task, "hi")
	candidate, err := a.qa.prepareOneBotFileCandidate(task, proposal)
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{candidate}
	if a.doc.ed.Doc.PlainText() != "hello world\nkeep this" {
		t.Fatal("preview modified document")
	}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.Doc.PlainText() != "hi world\nkeep this" || candidate.Changes[0].Status != "saved" {
		t.Fatalf("not applied/saved: %+v", candidate)
	}
	if _, err = os.Stat(filepath.Join(a.cfg.ConfigDir, "onebot", "tasks", task.ID+".json")); err != nil {
		t.Fatal("recovery record missing", err)
	}
	a.doc.ed.Undo()
	if a.doc.ed.Doc.PlainText() != "hello world\nkeep this" {
		t.Fatal("one undo did not restore original")
	}
}

func TestOneBotStaleSelectionAndDuplicateApplyDoNotOverwrite(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	task := editTestTask(t, a)
	c, err := a.qa.prepareOneBotFileCandidate(task, selectionFileDraft(task, "hi"))
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{c}
	a.doc.ed.ReplaceText(nil, "mine")
	if err = a.qa.applyOneBotCandidate(task, 0, -1); !errors.Is(err, model.ErrEditConflict) {
		t.Fatalf("stale target accepted: %v", err)
	}
}

func TestOneBotNewDocumentUsesExclusiveCreation(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	task := editTestTask(t, a)
	task.NewPath = "Created.md"
	c, err := a.qa.prepareOneBotFileCandidate(task, agent.FileDraft{Changes: []agent.FileChange{{Path: "Created.md", After: "# Created\n\ntext", Created: true}}})
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{c}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err != nil {
		t.Fatal(err)
	}
	if err = a.qa.applyOneBotCandidate(task, 0, -1); err == nil {
		t.Fatal("repeated application accepted")
	}
	raw, err := os.ReadFile(filepath.Join(root(a), "Created.md"))
	if err != nil || string(raw) != "# Created\n\ntext" {
		t.Fatalf("new document: %q %v", raw, err)
	}
}

func selectionFileDraft(task *oneBotTask, after string) agent.FileDraft {
	return agent.FileDraft{Changes: []agent.FileChange{{Path: task.Targets[0].Path, Before: task.Targets[0].Markdown, After: after}}}
}

// The selection bar's OneBot menu runs a preset on exactly the selected
// text: the instruction reaches the model and the edit lands in place.
func TestSelectionBarOneBotPresetEditsSelection(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "key")
	a.prefs.AI.Direct = true
	a.qa.sync = true
	editTestTask(t, a)
	var prompt string
	a.qa.provider = func(agent.Settings) agent.Provider {
		return oneBotProviderFunc(func(ctx context.Context, r agent.Request, delta func(agent.Delta)) (agent.Response, error) {
			if len(r.Messages) == 1 {
				prompt = r.Messages[0].Text
				data, _ := json.Marshal(map[string]string{"path": a.doc.rel, "oldString": "hello", "newString": "hi"})
				return agent.Response{Message: agent.Message{Calls: []agent.ToolCall{{ID: "edit", Name: "edit", Args: string(data)}}}, FinishReason: "tool_calls"}, nil
			}
			return agent.Response{Message: agent.Message{Text: "done"}, FinishReason: "stop"}, nil
		})
	}
	a.qa.input = "unsent draft"
	a.doc.ed.RequestFocus = true
	tt.Frame()
	a.doc.ed.SetSelection(0, 5)
	tt.Frame()
	if err := tt.Click("OneBot 修改选区"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("润色"); err != nil {
		t.Fatal(err, tt.Menu())
	}
	tt.Frame()
	if !strings.Contains(prompt, "润色选中内容") {
		t.Fatalf("preset instruction missing from prompt: %q", prompt)
	}
	if got := a.doc.ed.Doc.PlainText(); got != "hi world\nkeep this" {
		t.Fatalf("selection edit: %q", got)
	}
	if !a.qa.open || a.qa.editScope != "selection" || a.qa.input != "unsent draft" {
		t.Fatalf("OneBot state: open=%v scope=%q input=%q", a.qa.open, a.qa.editScope, a.qa.input)
	}
}
