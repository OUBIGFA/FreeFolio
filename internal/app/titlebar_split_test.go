package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/database"
	model "github.com/OUBIGFA/FreeFolio/internal/document"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Every document toolbar must offer splitting, including formats without a mind map.
func TestTitlebarSplitAcrossFormats(t *testing.T) {
	arcmd, err := model.New().Encode()
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.EncodeDatabaseMarkdown(database.NewDefault("Records", database.SourceConfig{Type: database.SourceFolder}), nil)
	if err != nil {
		t.Fatal(err)
	}
	board, err := whiteboard.NewScene().Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, files := range []bool{false, true} {
		for _, fixture := range []struct {
			name, content string
			source        bool
		}{
			{"Note.md", "# Note\n", false},
			{"Source.md", "# Source\n", true},
			{"Note.arcmd", arcmd, false},
			{"Canvas.excalidraw", board, false},
			{"Records.db.md", db, false},
			{"Archive.mdz", string(buildTestProjectMDZ(t)), false},
			{"Paper.pdf", string(samplePDF(t)), false},
			{"config.yaml", "name: demo\n", false},
			{"Letter.rtf", `{\rtf1\ansi Hello\par}`, false},
		} {
			t.Run(map[bool]string{false: "space/", true: "files/"}[files]+fixture.name, func(t *testing.T) {
				a, tt := newTestApp(t)
				tt.SetSize(1500, 850)
				full := filepath.Join(root(a), fixture.name)
				if files {
					full = externalFile(t, fixture.name, fixture.content)
					if !a.openSystemFile(full) {
						t.Fatal("open loose file")
					}
				} else {
					if err := os.WriteFile(full, []byte(fixture.content), 0600); err != nil {
						t.Fatal(err)
					}
					a.open(fixture.name)
				}
				first := a.doc
				if first == nil || first.loadErr != nil {
					t.Fatalf("failed to open %s", fixture.name)
				}
				if fixture.source {
					first.toggleMode()
				}
				settle(tt)
				if err := tt.Click("添加分屏"); err != nil {
					t.Fatal(err)
				}
				settle(tt)
				if a.split == nil || len(a.split.panes) != 2 || a.split.panes[0].doc != first || a.palette.open {
					t.Fatal("split did not preserve the document and open home")
				}
				if got, err := os.ReadFile(full); err != nil || string(got) != fixture.content {
					t.Fatalf("splitting changed the original file: %v", err)
				}
			})
		}
	}
}

func TestNewSplitShowsHomeWithoutSearch(t *testing.T) {
	for _, files := range []bool{false, true} {
		t.Run(map[bool]string{false: "space", true: "files"}[files], func(t *testing.T) {
			a, tt := newTestApp(t)
			if files && !a.openSystemFile(externalFile(t, "First.md", "# First\n")) {
				t.Fatal("open file")
			}
			settle(tt)
			first := a.doc
			if err := tt.Click("添加分屏"); err != nil {
				t.Fatal(err)
			}
			settle(tt)
			if a.palette.open || a.doc != nil || a.split.panes[0].doc != first {
				t.Fatal("adding a split must show its home without opening search or replacing the first document")
			}
			if !files {
				if !tt.HasText("从侧边栏选择文档，或搜索空间内容") || !tt.HasText("新建白板") {
					t.Fatal("space home is missing")
				}
				// Clicking an inactive home must target that pane, not the active editor.
				a.focusPane(0, false)
				settle(tt)
				if err := tt.Click("新建白板"); err != nil {
					t.Fatal(err)
				}
				settle(tt)
				if a.split.panes[0].doc != first || a.split.panes[1].doc == nil || a.split.panes[1].doc.board == nil {
					t.Fatal("home action opened in the wrong pane")
				}
			} else if !tt.HasText("没有打开的文件") {
				t.Fatal("file browser home is missing")
			}
		})
	}
}

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

func TestTitlebarSplitCanKeepAddingPanes(t *testing.T) {
	for _, files := range []bool{false, true} {
		t.Run(map[bool]string{false: "space", true: "files"}[files], func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetTitleBar(windowsTitleBar)
			tt.SetSize(900, 760)
			if files && !a.openSystemFile(externalFile(t, "First.md", "# First\n")) {
				t.Fatal("open loose file")
			}
			first := a.doc
			settle(tt)
			for count := 2; count <= splitLimit; count++ {
				if err := tt.Click("添加分屏"); err != nil {
					t.Fatalf("adding pane %d: %v", count, err)
				}
				settle(tt)
				if a.split == nil || len(a.split.panes) != count || a.split.panes[0].doc != first {
					t.Fatalf("adding pane %d lost the document or failed", count)
				}
				// Empty panes still need the shortcut after dismissing the picker.
				tt.Key(0, ui.KeyEscape)
				settle(tt)
			}
			button, ok := tt.Access("添加分屏")
			if !ok || !button.Disabled {
				t.Fatal("split shortcut must stay visible but disabled at the limit")
			}
			tt.ClickAt(button.Bounds.X+button.Bounds.W/2, button.Bounds.Y+button.Bounds.H/2)
			settle(tt)
			if len(a.split.panes) != splitLimit || a.palette.open {
				t.Fatal("disabled split shortcut changed the workspace")
			}
		})
	}
}
