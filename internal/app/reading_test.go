package app

import (
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/editor"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestReadingSettingsAndToolbarLock(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	a.openSettings(tabAppearance)
	tt.Frame()
	// The document group is below the preview: scroll to it, then step
	// each number input up with the keyboard.
	step := func(label string) {
		t.Helper()
		scrollIntoView(tt, label, 1200, 760)
		// The row's label shows first; the input named by it comes after.
		var in ui.AccessInfo
		ok := false
		for _, n := range tt.AccessAll() {
			if n.Name == label {
				in, ok = n, true
			}
		}
		if !ok {
			t.Fatalf("no %s input", label)
		}
		tt.ClickAt(in.Bounds.X+8, in.Bounds.Y+in.Bounds.H/2)
		tt.Key(0, ui.KeyUp)
		tt.Frame()
	}
	step("正文字号")
	if a.prefs.ReadingSize() != 17 {
		t.Fatal("font size", a.prefs.ReadingSize())
	}
	step("行距")
	p, err := (prefs.Store{Path: a.prefStore.Path}).Load()
	if err != nil || p.FontSize != 17 || p.LineHeight != a.prefs.LineHeight {
		t.Fatal("preferences not saved", p, err)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if err := tt.Click("编辑中，点击设为只读"); err != nil {
		t.Fatal(err)
	}
	if !a.doc.ed.ReadOnly {
		t.Fatal("not locked")
	}
	if err := tt.Click("只读，点击恢复编辑"); err != nil {
		t.Fatal(err)
	}
	if a.doc.ed.ReadOnly {
		t.Fatal("not unlocked")
	}
}

func TestDocumentLockKeepsPointerShapes(t *testing.T) {
	for _, mode := range []string{"space", "files"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1400, 850)
			const text = "# Pointer\n\nPlain text for selecting.\n\n[Link](https://example.com)"
			if mode == "files" {
				a.prefs.Files.ReadOnly = false
				if !a.openSystemFile(externalFile(t, "Pointer.md", text)) {
					t.Fatal("open file")
				}
			} else {
				if err := vault.Write(root(a), "Pointer.md", text); err != nil {
					t.Fatal(err)
				}
				a.open("Pointer.md")
			}
			settle(tt)
			for _, locked := range []bool{false, true, false} {
				if a.doc.ed.ReadOnly != locked {
					label := "编辑中，点击设为只读"
					if !locked {
						label = "只读，点击恢复编辑"
					}
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
					settle(tt)
				}
				bounds, ok := tt.Find("文档")
				if !ok {
					t.Fatal("missing document surface")
				}
				r, _, ok := a.doc.ed.BoundsForRange(ui.TextInputRange{Start: 10, End: 14})
				if !ok {
					t.Fatal("missing text geometry")
				}
				link, _, ok := a.doc.ed.BoundsForRange(ui.TextInputRange{Start: a.doc.ed.Len() - 2, End: a.doc.ed.Len() - 2})
				if !ok {
					t.Fatal("missing link geometry")
				}
				for _, sample := range []struct {
					name   string
					x, y   float32
					cursor ui.Cursor
				}{
					{"text", r.X + r.W/2, r.Y + r.H/2, ui.CursorText},
					{"link", link.X + link.W/2, link.Y + link.H/2, ui.CursorPointer},
					{"right margin", bounds.W - 20, r.Y + r.H/2, ui.CursorDefault},
					{"below content", bounds.W / 2, 500, ui.CursorDefault},
				} {
					tt.Move(bounds.X+sample.x, bounds.Y+sample.y)
					tt.Frame()
					if got := tt.Cursor(); got != sample.cursor {
						t.Errorf("locked=%v %s: cursor=%v want=%v", locked, sample.name, got, sample.cursor)
					}
				}
				if locked {
					before := a.doc.ed.Text()
					tt.ClickAt(bounds.X+r.X+r.W/2, bounds.Y+r.Y+r.H/2)
					tt.Type("locked input")
					if a.doc.ed.Text() != before {
						t.Fatal("locked document accepted an edit")
					}
				}
			}
		})
	}
}

func TestWikiPreviewUsesUnsavedTarget(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	doc := editor.ImportMarkdown("# Target\n\nSaved text")
	data, _ := doc.Encode()
	if err := vault.Write(root(a), "Target.arcmd", data); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	from := a.doc
	a.open("Target.arcmd")
	a.doc.ed.ReplaceText(nil, "Unsaved ")
	p := a.links.previewWikiFor(from, "Target")
	if p.Missing || p.Text == "" || p.Title != "Target" {
		t.Fatal(p)
	}
	if p.Text[:8] != "Unsaved " {
		t.Fatal("preview missed live text", p.Text)
	}
	missing := a.links.previewWikiFor(from, "Does not exist")
	if !missing.Missing {
		t.Fatal("missing state")
	}
}
