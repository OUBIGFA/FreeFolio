package app

import (
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
)

func TestOneBotReviewUIKeepsSelectionAfterFocusAndApplies(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "key")
	task := editTestTask(t, a)
	a.qa.editTask = task
	a.qa.editMode = true
	a.qa.open = true
	c, err := a.qa.prepareOneBotFileCandidate(task, selectionFileDraft(task, "hi"))
	if err != nil {
		t.Fatal(err)
	}
	task.Candidates = []*oneBotCandidate{c}
	a.qa.attachOneBotTask(task)
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()
	if !tt.HasText("OneBot") || !tt.HasText("应用修改") || !tt.HasText("1 个文件 · 1 处修改") {
		t.Fatalf("review controls missing: %v", tt.Texts())
	}
	a.doc.ed.SetSelection(16, 16)
	if err = tt.Click("应用修改"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.ed.Doc.PlainText() != "hi world\nkeep this" || !tt.HasText("已保存 · 1 个文件 · 1 处修改") {
		t.Fatalf("apply at wrong caret: %s", a.doc.ed.Doc.PlainText())
	}
}

func TestOneBotSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect OneBot")
	for _, mode := range []string{"review", "settings", "review-dark", "review-narrow", "review-diff", "alternatives", "review-wide"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			setupOneBot(a, "key")
			tt.SetSize(1280, 850)
			if mode == "settings" {
				a.prefs.AI.Current().Name = "OpenAI"
				a.prefs.AI.Current().Models = []agent.ModelInfo{{ID: "writing-model", Name: "Writing", Enabled: true, Capabilities: agent.Capabilities{Tools: agent.Supported, Context: 128000}}, {ID: "fast-model"}, {ID: "reasoning-model", Enabled: true}}
				a.openSettings(tabAI)
			} else {
				a.doc.ed.ReadOnly = false
				original := "我们的团队将会在下周对目前的计划进行一次重新评估，并根据最新反馈进行相应调整。"
				a.doc.ed.Engine.ReplaceDocument(editor.ImportMarkdown(original + "\n\n评估结果将在周五的例会上讨论，负责人会提前整理用户反馈。\n"))
				a.doc.ed.SetSelection(0, model.Len(original))
				task, err := a.qa.captureOneBotTask("selection")
				if err != nil {
					t.Fatal(err)
				}
				a.qa.editTask = task
				a.qa.editMode = true
				a.qa.open = true
				task.Instruction = "保持原意，让表达更简洁。"
				c, err := a.qa.prepareOneBotFileCandidate(task, selectionFileDraft(task, "团队将于下周根据最新反馈重新评估并调整计划。"))
				if err != nil {
					t.Fatal(err)
				}
				task.Candidates = []*oneBotCandidate{c}
				if mode == "review-diff" {
					task.ShowDiff = true
				}
				if mode == "alternatives" {
					for _, text := range []string{"下周，团队将结合最新反馈评估计划并作调整。", "团队计划下周重新评估方案，纳入最新反馈。"} {
						alternative, err := a.qa.prepareOneBotFileCandidate(task, selectionFileDraft(task, text))
						if err != nil {
							t.Fatal(err)
						}
						task.Candidates = append(task.Candidates, alternative)
					}
				}
				a.qa.attachOneBotTask(task)
				if mode == "review-dark" {
					tt.SetDark(true)
				}
				if mode == "review-wide" {
					a.prefs.OneBotWidth = 560
				}
				if mode == "review-narrow" {
					tt.SetSize(760, 620)
				}
			}
			tt.Frame()
			tt.Advance(slide)
			tt.Frame()
			savePNG(t, filepath.Join(dir, "onebot-"+mode+".png"), tt)
		})
	}
}

// Beside OneBot in a small window the document toolbar drops optional controls
// instead of squeezing the OneBot and more buttons below their hit size.
func TestNarrowToolbarKeepsOneBotButtons(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "key")
	a.qa.open = true
	tt.SetSize(760, 620)
	tt.Frame()
	tt.Advance(slide)
	tt.Frame()
	for _, label := range []string{"更多", "OneBot"} {
		if r, ok := tt.Find(label); !ok || r.W < 29 {
			t.Fatalf("%s squeezed: %v found=%v", label, r, ok)
		}
	}
}

func TestOneBotCardMatchesEditorCardHeight(t *testing.T) {
	for _, left := range []bool{false, true} {
		t.Run(map[bool]string{false: "sidebar-right", true: "sidebar-left"}[left], func(t *testing.T) {
			a, tt := newTestApp(t)
			setupOneBot(a, "key")
			a.prefs.SidebarLeft = left
			a.qa.open = true
			tt.SetSize(1280, 820)
			settle(tt)
			resizer, ok := tt.Find("调整 OneBot 宽度")
			if !ok {
				t.Fatal("missing OneBot resizer")
			}
			// Both the editor card and OneBot card start directly below the 52-DIP top bar.
			if resizer.Y != mainBarHeight {
				t.Fatalf("OneBot resizer top = %v, want %v", resizer.Y, mainBarHeight)
			}
			if resizer.H != 820-mainBarHeight-8 {
				t.Fatalf("OneBot resizer height = %v, want %v", resizer.H, 820-mainBarHeight-8)
			}
			// Visually verify that the top border of the editor card and OneBot card
			// sit on the exact same row (y = mainBarHeight).
			img := tt.Image()
			editorX := int(resizer.X) - 40
			oneBotX := int(resizer.X+resizer.W) + 40
			topY := int(mainBarHeight)
			if img.At(editorX, topY) != img.At(oneBotX, topY) {
				t.Fatalf("top border mismatch at y=%d: editor=%v onebot=%v", topY, img.At(editorX, topY), img.At(oneBotX, topY))
			}
			if img.At(editorX, topY-2) != img.At(oneBotX, topY-2) {
				t.Fatalf("chrome above card mismatch at y=%d: editor=%v onebot=%v", topY-2, img.At(editorX, topY-2), img.At(oneBotX, topY-2))
			}
		})
	}
}
