package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

// All document hosts must use the content-aware outline, even on cold open.
func TestOutlineWideContentAcrossModes(t *testing.T) {
	text := "# Wide document\n\n## Table\n\n| A | B | C | D |\n| --- | --- | --- | --- |\n|" + strings.Repeat(strings.Repeat("wide text ", 8)+"|", 4) + "\n\n## Notes\n\nBody text.\n"
	for _, mode := range []string{"space", "folded", "focus", "files", "split", "split-vertical", "window"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetTitleBar(windowsTitleBar)
			tt.SetSize(1800, 900)
			if err := vault.Write(root(a), "Wide.md", text); err != nil {
				t.Fatal(err)
			}
			a.open("Wide.md")
			docs := []*document{a.doc}
			switch mode {
			case "folded":
				a.sidebarHidden = true
			case "focus":
				a.toggleFocus()
			case "files":
				if !a.openSystemFile(externalFile(t, "Wide.md", text)) {
					t.Fatal("open loose file")
				}
				docs = []*document{a.doc}
			case "split", "split-vertical":
				if err := vault.Write(root(a), "Other.md", text); err != nil {
					t.Fatal(err)
				}
				a.openSplit(a.state.ActiveSpace(), "Other.md", false, false)
				a.split.vertical = mode == "split-vertical"
				if !a.split.vertical {
					tt.SetSize(3400, 900)
				}
				docs = []*document{a.split.panes[0].doc, a.split.panes[1].doc}
			case "window":
				var made *fakeWindow
				a.newWindow = func(title string, view func(*ui.Context), close func() bool) windowHandle {
					made = &fakeWindow{title: title, view: view, close: close}
					return made
				}
				a.openInWindow(a.state.ActiveSpace(), "Wide.md")
				tt = ui.NewTester(made.view, 1800, 900)
				tt.SetTitleBar(windowsTitleBar)
			}
			settle(tt)
			for _, d := range docs {
				if d.ed.OutlineOpen() {
					t.Fatalf("outline covers the wide table in %s", d.rel)
				}
			}
			if _, ok := tt.Find("展开目录"); !ok {
				t.Fatal("automatic collapse removed the outline entry point")
			}
			if a.prefs.OutlineClosed {
				t.Fatal("automatic collapse changed the saved outline preference")
			}
			if os.Getenv("FREEFOLIO_SNAPSHOT") != "" {
				savePNG(t, filepath.Join(snapshotDir(t, ""), "outline-wide-"+mode+".png"), tt)
			}
		})
	}
}
