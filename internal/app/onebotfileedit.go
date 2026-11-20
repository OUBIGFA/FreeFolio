package app

import (
	"errors"
	"path"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/agent"
	model "github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/history"
	"github.com/OUBIGFA/FreeMD/internal/i18n"
	"github.com/OUBIGFA/FreeMD/internal/vault"
)

func oneBotEditFiles(task *oneBotTask) []agent.EditFile {
	var files []agent.EditFile
	for _, target := range task.Targets {
		if !target.Derived {
			files = append(files, agent.EditFile{Path: target.Path, Content: target.Markdown})
		}
	}
	return files
}
func oneBotEditPaths(files []agent.EditFile, newPaths []string) []string {
	paths := append([]string(nil), newPaths...)
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

// The model sees only OpenCode file tools. This adapter projects their final
// snapshot into FreeMD's native document transactions and review rows.
func (a *App) prepareOneBotFileCandidate(task *oneBotTask, draft agent.FileDraft) (*oneBotCandidate, error) {
	c := &oneBotCandidate{Title: i18n.T("文档修改"), Text: draft.Summary, Complete: true}
	base := map[string]oneBotTarget{}
	for _, t := range task.Targets {
		if !t.Derived {
			base[t.Path] = t
		}
	}
	for _, change := range draft.Changes {
		if change.Created {
			if change.Path != task.NewPath || task.NewPath == "" {
				return nil, errors.New(i18n.T("新文档路径未获指定"))
			}
			sp := a.state.Get(task.SpaceID)
			if sp == nil || sp.IsExternal() {
				return nil, errors.New(i18n.T("请在空间中创建文档"))
			}
			if _, err := vault.Resolve(sp.Root, change.Path); err != nil {
				return nil, err
			}
			if !strings.EqualFold(path.Ext(change.Path), ".md") {
				return nil, errors.New(i18n.T("新文档须使用 .md 扩展名"))
			}
			c.Changes = append(c.Changes, &oneBotChange{Path: change.Path, NewText: change.After, Operation: model.EditOperation{Kind: "create_document"}, Status: "pending", Diff: history.Diff(nil, strings.Split(change.After, "\n"))})
			continue
		}
		target, ok := base[change.Path]
		if !ok || change.Before != target.Markdown {
			return nil, model.ErrEditConflict
		}
		var ops []model.EditOperation
		if target.Whole || task.Scope == "section" {
			var err error
			ops, err = editor.MarkdownFileEdits(target.Native, change.After)
			if err != nil {
				return nil, err
			}
			if !target.Whole {
				for _, op := range ops {
					if op.Kind == "update_metadata" || op.Kind == "update_source" {
						return nil, errors.New(i18n.T("文档属性修改需要整篇文档范围"))
					}
				}
			}
		} else {
			op := model.EditOperation{Target: target.Target, Text: change.After}
			if target.Plain {
				op.Kind = "replace_text"
			} else {
				fragment := editor.ImportMarkdown(change.After)
				if model.PageMetadataOf(fragment) != (model.PageMetadata{}) {
					return nil, errors.New(i18n.T("文档属性修改需要整篇文档范围"))
				}
				op.Kind = "paste_fragment"
				op.Fragment = fragment
				if change.After == "" {
					op.Kind = "delete"
				}
				// A literal selection rewrite preserves native inline marks. A
				// formatted Markdown replacement uses the existing paste path.
				if len(fragment.Blocks) == 1 && fragment.Blocks[0].Kind == "paragraph" && len(fragment.Blocks[0].Comments) == 0 {
					plain := true
					for _, r := range fragment.Blocks[0].Runs {
						if r.Style != (model.Style{}) {
							plain = false
						}
					}
					oldPlain := true
					for _, b := range target.Native.Blocks {
						for _, r := range b.Runs {
							if r.Style != (model.Style{}) {
								oldPlain = false
							}
						}
					}
					if plain && oldPlain {
						op.Kind = "replace_text"
						op.Text = fragment.Blocks[0].Text()
					}
				}
			}
			ops = []model.EditOperation{op}
		}
		for _, op := range ops {
			t := target
			if target.Whole || task.Scope == "section" {
				t.ID = model.NewID()
				t.Target = op.Target
				t.Derived = true
				t.Whole = false
			}
			// Bind and validate only changed structures; unrelated edits made
			// while the model was working do not invalidate the entire file.
			d, err := a.oneBotTargetDoc(task, &t)
			if err != nil {
				return nil, err
			}
			op.Target = t.Target
			work := model.NewEngine(d.ed.Committed())
			if _, err = work.ApplyAI([]model.EditOperation{op}); err != nil {
				return nil, err
			}
			before, after := t.Target.Text, op.Text
			if op.Kind == "replace_snapshot" {
				fragment := model.New()
				fragment.Blocks = op.Target.Blocks
				before = editor.ExportMarkdown(fragment)
				fragment.Blocks = op.Blocks
				after = editor.ExportMarkdown(fragment)
				op.Text = after
			}
			if op.Kind == "paste_fragment" {
				before, after = target.Markdown, change.After
			}
			if op.Kind == "update_metadata" || op.Kind == "update_source" {
				before = editor.ExportMarkdown(target.Native)
				after = change.After
			}
			t.Markdown = before
			if t.Derived {
				task.Targets = append(task.Targets, t)
			}
			c.Changes = append(c.Changes, &oneBotChange{TargetID: t.ID, Path: change.Path, Operation: op, Status: "pending", Diff: history.Diff(strings.Split(strings.TrimRight(before, "\n"), "\n"), strings.Split(strings.TrimRight(after, "\n"), "\n"))})
		}
	}
	return c, nil
}
