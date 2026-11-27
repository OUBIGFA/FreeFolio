package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTitlebarSplitBeforeMindMapInBothModes(t *testing.T) {
	for _, files := range []bool{false, true} {
		t.Run(map[bool]string{false: "space", true: "files"}[files], func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1500, 850)
			if files && !a.openSystemFile(externalFile(t, "First.md", "# First\n\nBody\n")) {
				t.Fatal("open loose file")
			}
			settle(tt)
			button, ok := tt.Find("添加分屏")
			mind, hasMind := tt.Find("思维导图")
			if !ok || !hasMind || button.X+button.W > mind.X {
				t.Fatalf("split control must appear before mind map: split=%+v mind=%+v", button, mind)
			}
			first := a.doc
			if err := tt.Click("添加分屏"); err != nil {
				t.Fatal(err)
			}
			settle(tt)
			if a.split == nil || len(a.split.panes) != 2 || a.split.panes[0].doc != first {
				t.Fatal("split did not preserve first document")
			}
			if files {
				if !a.openSystemFile(externalFile(t, "Second.md", "# Second\n")) {
					t.Fatal("open second loose file")
				}
				settle(tt)
				if a.split.panes[1].doc != a.doc || !tt.HasText("关闭分屏 1") || !tt.HasText("关闭分屏 2") {
					t.Fatal("file browser did not render both panes")
				}
				first.ed.SetSelection(first.ed.Len(), first.ed.Len())
				first.ed.ReplaceText(nil, " retained")
				if !a.showFileTab(first.rel) || a.doc != first || !strings.Contains(first.ed.Text(), "retained") {
					t.Fatal("switching tabs duplicated or discarded the existing pane")
				}
				a.closePane(1)
				tt.Frame()
				if a.split != nil || a.doc != first {
					t.Fatalf("closing file pane did not preserve remaining editor: split=%p doc=%p first=%p notices=%v", a.split, a.doc, first, a.notices)
				}
				if !a.leaveFileMode() || a.files != nil {
					t.Fatal("returning to space failed")
				}
			}
		})
	}
}

func TestFileSplitDoesNotOverwriteSuspendedSpace(t *testing.T) {
	a, tt := splitTestApp(t)
	original, doc := a.split, a.doc
	if !a.openSystemFile(externalFile(t, "Outside.md", "# Outside\n")) {
		t.Fatal("open")
	}
	a.addSplitPane()
	settle(tt)
	if !a.leaveFileMode() || a.split != original || a.doc != doc {
		t.Fatal("file split replaced the suspended space group")
	}
	if a.palette.open {
		tt.Key(0, ui.KeyEscape)
	}
}
