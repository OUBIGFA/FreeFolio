package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/OUBIGFA/FreeMD/internal/document"
	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/history"
	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

const linksSample = "# 旅行计划\n\n这次去 [[Ideas]] 里提到的海边，顺便整理 [[Welcome|欢迎页]]。#旅行 #计划/2026\n\n## 预算\n\n每天的花费大约是：\n\n$$\n\\sum_{i=1}^{n} c_i = \\frac{a+b}{2} + \\sqrt{x^2+y^2}\n$$\n\n## 公式\n\n$$\nf(x) = \\begin{cases} x^2 & x > 0 \\\\ 0 & \\text{否则} \\end{cases}\n$$\n\n$$\n\\lim_{x \\to 0} \\frac{\\sin x}{x} = 1, \\quad \\int_0^1 e^{-x}\\,dx, \\quad \\begin{pmatrix} a & b \\\\ c & d \\end{pmatrix}\n$$\n"

// TestFeaturesSnapshot renders the new features for review.
func TestFeaturesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to render the features")
	for _, name := range []string{"links-math", "links-math-dark", "wiki-complete", "focus", "versions", "trash", "shortcuts", "backlinks", "pdf-page"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			if name == "links-math-dark" {
				tt.SetDark(true)
			}
			doc := editor.ImportMarkdown(linksSample)
			doc.Icon, doc.Cover = "🏖️", "gradient:sea"
			text, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "旅行计划.arcmd", text); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.refreshSearch()
			a.open("旅行计划.arcmd")
			switch name {
			case "wiki-complete":
				a.doc.ed.Engine.InsertAt(len(a.doc.ed.Doc.Blocks), model.NewBlock("paragraph", ""))
				caretAtEnd(a, tt)
				tt.Type("下一站 [[")
			case "focus":
				a.prefs.Typewriter = true
				a.toggleFocus()
				focusDocument(a, tt)
				a.doc.ed.SetSelection(30, 30)
				a.doc.ed.RequestFocus = true
			case "versions":
				caretAtEnd(a, tt)
				tt.Type("再加一句。")
				saveSettle(tt)
				tt.Type("又改了一处。")
				saveSettle(tt)
				a.openVersions()
				tt.Frame()
				a.selectVersion(1)
			case "trash":
				a.trashFile = os.RemoveAll
				a.trash("Journal")
				a.openTrash()
			case "shortcuts":
				a.openSettings(tabShortcuts)
			case "backlinks":
				a.open("Journal/Ideas.md")
			case "pdf-page":
				ed := editor.FromDocument(a.doc.ed.Committed())
				theme := documentTheme(false)
				o := editor.Options{Width: paperWidth, Theme: theme, Page: pageStyle(false), LoadImage: a.doc.imageResource}
				body := float32(paperHeight - 2*paperMargin)
				var tops []float32
				ui.Render(func(c *ui.Context) { tops = ed.Paginate(o, body) }, paperWidth, paperHeight, 1)
				img := ui.Render(func(c *ui.Context) {
					ui.Box(c).Fill().Background(theme.Background).Draw(func(p *ui.Painter, r ui.Rect) {
						ed.PaintPage(p, ui.Rect{X: r.X, Y: r.Y + paperMargin, W: r.W, H: body}, tops[0], tops[0]+body, theme)
					})
				}, paperWidth, paperHeight, 1)
				f, err := os.Create(filepath.Join(dir, "feature-pdf-page.png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				png.Encode(f, img)
				return
			}
			for range 4 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "feature-"+name+".png"), tt)
		})
	}
}

const v03CodeMermaidSample = `# v0.3 技术写作预览

` + "```go\n// BuildVectorPipeline executes per-language lexing and vector layout without wrapping long diagnostic lines.\nfunc BuildVectorPipeline(input string) (int, error) {\n\treturn len(input), nil\n}\n```\n\n```mermaid\nflowchart LR\n  A[Markdown] --> B{词法高亮}\n  B -->|Mermaid| C((矢量图表))\n  B -->|PDF| D[Type 3]\n```\n\n```mermaid\nsequenceDiagram\n  participant E as Editor\n  participant M as Mermaid\n  E->>+M: ParseAndLayout(src)\n  M-->>-E: Shapes & Paths\n```\n"

// TestV03FeaturesSnapshot renders syntax highlighting, unfolded code scroll,
// native Mermaid diagrams (light & dark), and version grapheme milestones.
func TestV03FeaturesSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to render the v0.3 features")
	for _, name := range []string{"code-mermaid-light", "code-mermaid-dark"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			a.prefs.CodeNoWrap = true
			if name == "code-mermaid-dark" {
				tt.SetDark(true)
			} else {
				tt.SetDark(false)
			}
			doc := editor.ImportMarkdown(v03CodeMermaidSample)
			text, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "v03Demo.arcmd", text); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.refreshSearch()
			a.open("v03Demo.arcmd")
			for range 4 {
				tt.Frame()
				tt.Advance(200 * time.Millisecond)
			}
			tt.Frame()
			savePNG(t, filepath.Join(dir, "feature-"+name+".png"), tt)
		})
	}

	t.Run("versions-milestone", func(t *testing.T) {
		a, tt := newTestApp(t)
		initial := "# 发布评审\n\n第一阶段完成 核心引擎 编写与自测。\n"
		doc := editor.ImportMarkdown(initial)
		text, err := doc.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if err := vault.Write(root(a), "Milestone.arcmd", text); err != nil {
			t.Fatal(err)
		}
		if _, err := history.SaveMilestone(root(a), "Milestone.arcmd", text, "定稿 v1", time.Now().Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.refreshSearch()
		a.open("Milestone.arcmd")
		tt.Frame()
		caretAtEnd(a, tt)
		for range len([]rune("编写与自测。")) {
			tt.Key(0, ui.KeyBackspace)
		}
		tt.Type("发布与验收。")
		saveSettle(tt)
		a.openVersions()
		tt.Frame()
		for i, ver := range a.versions.versions {
			if ver.Label == "定稿 v1" {
				a.selectVersion(i)
				break
			}
		}
		for range 4 {
			tt.Frame()
			tt.Advance(200 * time.Millisecond)
		}
		tt.Frame()
		savePNG(t, filepath.Join(dir, "feature-versions-milestone.png"), tt)
	})
}

// TestFoldAndSidebarSnapshot renders a long code block folded and opened,
// a space with a picture icon and the sidebar dragged wider.
func TestFoldAndSidebarSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to render code folds")
	a, tt := newTestApp(t)
	tt.SetSize(1240, 760)
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("fmt.Println(\"line %d\")", i))
	}
	os.WriteFile(filepath.Join(root(a), "Code.md"), []byte("# 代码\n\n```go\n"+strings.Join(lines, "\n")+"\n```\n\n后面的段落。\n"), 0o644)
	a.refreshTree()
	a.open("Code.md")
	settle(tt)
	writePNG(t, filepath.Join(dir, "code-folded.png"), tt.Image())
	a.doc.ed.ToggleCodeFold(a.doc.ed.Doc.Blocks[1].ID)
	settle(tt)
	writePNG(t, filepath.Join(dir, "code-open.png"), tt.Image())
	a.prefs.SidebarWidth = 360
	a.state.ActiveSpace().SetIcon("工作", "", "")
	settle(tt)
	writePNG(t, filepath.Join(dir, "sidebar-wide.png"), tt.Image())
}
