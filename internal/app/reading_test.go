package app

import (
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/prefs"
	"github.com/OUBIGFA/FreeMD/internal/vault"
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
	p := a.previewWikiFor(from, "Target")
	if p.Missing || p.Text == "" || p.Title != "Target" {
		t.Fatal(p)
	}
	if p.Text[:8] != "Unsaved " {
		t.Fatal("preview missed live text", p.Text)
	}
	missing := a.previewWikiFor(from, "Does not exist")
	if !missing.Missing {
		t.Fatal("missing state")
	}
}
