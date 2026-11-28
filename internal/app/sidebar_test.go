package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestSpaceHeaderHoverRequiresPins(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%v", dark), func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			sp := a.state.ActiveSpace()
			sp.Pinned = nil
			for _, pinned := range []bool{false, true} {
				if pinned {
					a.togglePin(sp, "Welcome.md")
				}
				tt.Move(400, 400)
				settle(tt)
				r, ok := tt.Find("空间标题")
				if !ok {
					t.Fatal("space header missing")
				}
				// Sample the header's blank padding, away from text and menu icons.
				x, y := int(r.X+r.W/2), int(r.Y+3)
				before := tt.Image().RGBAAt(x, y)
				tt.Move(r.X+r.W/2, r.Y+r.H/2)
				settle(tt)
				after := tt.Image().RGBAAt(x, y)
				if changed := before != after; changed != pinned {
					t.Fatalf("pinned=%v header hover changed=%v", pinned, changed)
				}
			}
		})
	}
}

func TestSidebarHierarchyAlignsFilesAndFolders(t *testing.T) {
	app, tester := newTestApp(t)
	spaceID := addSpace(t, app, "工作笔记", map[string]string{
		"📝 总览.md":               "",
		"📁 项目/📝 本周计划.md":        "",
		"📁 项目/📁 参考资料/📝 访谈记录.md": "",
	})
	app.switchSpace(spaceID)
	space := app.state.ActiveSpace()
	space.SetExpanded("📁 项目", true)
	space.SetExpanded("📁 项目/📁 参考资料", true)
	app.open("📁 项目/📝 本周计划.md")
	tester.SetPreferences(ui.Preferences{ReduceMotion: true})
	settle(tester)
	labelX := func(name string) float32 {
		t.Helper()
		bounds, found := tester.Find(name)
		if !found || bounds.W <= 0 || bounds.H <= 0 {
			t.Fatalf("missing visible tree label %q: %v", name, bounds)
		}
		return bounds.X
	}
	parent, child, grandchild := labelX("项目"), labelX("本周计划"), labelX("访谈记录")
	if rootFile, subfolder := labelX("总览"), labelX("参考资料"); rootFile != parent || subfolder != child {
		t.Errorf("siblings use different name columns: root file/folder %.0f/%.0f, nested file/folder %.0f/%.0f", rootFile, parent, child, subfolder)
	}
	if child-parent < 12 || grandchild-child != child-parent {
		t.Errorf("inconsistent indentation: parent %.0f, child %.0f, grandchild %.0f", parent, child, grandchild)
	}
	for name, level := range map[string]int{"📁 项目": 1, "📝 本周计划": 2, "📁 参考资料": 2, "📝 访谈记录": 3} {
		if item, found := tester.AccessByRole(ui.RoleTreeItem, name); !found || item.Level != level {
			t.Errorf("%q has accessible level %d, want %d", name, item.Level, level)
		}
	}
	if os.Getenv("FREEFOLIO_SNAPSHOT") != "" {
		savePNG(t, filepath.Join(snapshotDir(t, "sidebar hierarchy"), "sidebar-hierarchy.png"), tester)
	}
}

func TestSidebarCurrentDocumentHasNoAccentRail(t *testing.T) {
	app, tester := newTestApp(t)
	app.open("Journal/Ideas.md")
	settle(tester)
	item, found := tester.AccessByRole(ui.RoleTreeItem, "Ideas")
	if !found || !item.Selected {
		t.Fatal("the open document must remain selected")
	}
	start := int(item.Bounds.X) + rowIndent
	center := int(item.Bounds.Y + item.Bounds.H/2)
	frame := tester.Image()
	if frame.RGBAAt(start+2, center) != frame.RGBAAt(start+7, center) {
		t.Fatal("an accent rail interrupts the selected row fill")
	}
}

func TestSidebarDoubleClickDoesNotRename(t *testing.T) {
	for _, name := range []string{"Journal", "Ideas"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			sp := a.state.ActiveSpace()
			for click := 1; click <= 2; click++ {
				if err := tt.Click(name); err != nil {
					t.Fatal(err)
				}
				if a.renaming != "" || tt.Focused("名称") {
					t.Fatalf("click %d started renaming %q", click, a.renaming)
				}
				if name == "Journal" {
					if sp.IsExpanded("Journal") != (click == 2) {
						t.Fatalf("click %d did not toggle the folder", click)
					}
				} else if a.doc == nil || a.doc.rel != "Journal/Ideas.md" {
					t.Fatalf("click %d did not open the document", click)
				}
			}
			// Renaming remains an explicit action in the row's menu.
			if err := tt.RightClick(name); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("重命名"); err != nil {
				t.Fatal(err)
			}
			if a.renaming == "" || !tt.Focused("名称") {
				t.Fatal("the context menu did not focus the rename field")
			}
		})
	}
}

func TestSidebarWidthAndPinning(t *testing.T) {
	a, tt := newTestApp(t)
	back, ok := tt.Find("新建空间")
	if !ok || back.X < 1200-sidebarWidth || a.sidebarHidden {
		t.Fatalf("default sidebar: back %+v, hidden %v", back, a.sidebarHidden)
	}
	tt.Click("收起侧边栏")
	tt.Advance(time.Second)
	folded, _ := tt.Find("文档路径")
	tt.Move(1197, 400)
	if !a.peek {
		t.Fatal("edge hover did not start opening without another input")
	}
	// Cross into the panel before its opening animation has finished.
	tt.Move(1020, 400)
	tt.Advance(time.Second)
	if !a.peek || !tt.HasText("Journal") {
		t.Fatal("moving ahead of the animation closed the panel")
	}
	floating, _ := tt.Find("文档路径")
	if floating != folded {
		t.Fatalf("peek moved the document: %+v -> %+v", folded, floating)
	}
	tt.Move(500, 400)
	tt.Advance(peekDelay / 2)
	tt.Move(1020, 400)
	tt.Advance(peekDelay)
	if !a.peek {
		t.Fatal("briefly leaving the floating panel made it disappear")
	}
	if err := tt.Click("固定侧边栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Move(900, 400)
	tt.Advance(time.Second)
	if a.sidebarHidden || a.peek || !tt.HasText("Journal") {
		t.Fatal("pinned sidebar did not remain open")
	}
}

func TestSidebarPeekRenameAndModal(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Move(1197, 400)
	if !a.peek || !tt.HasText("Journal") {
		t.Fatal("reduced-motion peek did not open immediately")
	}
	if err := tt.RightClick("Ideas"); err != nil {
		t.Fatal(err)
	}
	tt.Move(450, 400)
	tt.Advance(time.Second)
	if err := tt.ChooseMenuItem("重命名"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	if !a.peek || !tt.Focused("名称") {
		t.Fatal("rename lost its floating sidebar or focus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(time.Second)
	if a.peek || a.renaming != "" {
		t.Fatal("canceling rename did not release the sidebar")
	}
	tt.Key(ui.Cmd, ui.KeyP)
	tt.Move(1197, 400)
	if a.peek || !a.palette.open {
		t.Fatal("edge hover opened a sidebar behind the command bar")
	}
}

func TestSpaceSwitchWhilePeeking(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil // a favorite would stay open across the switch
	first := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Advance(time.Second)
	tt.Move(1197, 400)
	tt.Advance(time.Second)
	if err := tt.Click("切换到 Work"); err != nil {
		t.Fatal(err)
	}
	if a.state.ActiveSpace() != work || a.doc != nil || !a.sidebarHidden || !a.peek {
		t.Fatal("space switch changed the peek mode or showed another space's document")
	}
	if err := tt.Click("切换到 Personal"); err != nil {
		t.Fatal(err)
	}
	if a.state.ActiveSpace() != first || a.doc.rel != "Welcome.md" || !a.peek {
		t.Fatal("switching back did not restore Personal in the floating sidebar")
	}
	tt.Move(900, 400)
	tt.Advance(time.Second)
	if a.peek || !a.sidebarHidden || tt.HasText("Journal") {
		t.Fatal("space switch pinned the floating sidebar")
	}
}

func TestSpaceBarOverflowAndKeyboard(t *testing.T) {
	a, tt := newTestApp(t)
	for i := 1; i < 12; i++ {
		a.state.Add(fmt.Sprintf("Space %d", i), t.TempDir())
	}
	tt.SetSize(640, 420)
	for _, label := range []string{"新建空间", "资料库"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 640-sidebarWidth || r.X+r.W > 640 {
			t.Fatalf("%s overflowed the sidebar: %+v", label, r)
		}
	}
	if err := tt.Click("切换到 Personal"); err != nil {
		t.Fatal(err)
	}
	// The active icon opens its picker. Dismissing it must return the
	// keyboard to the space bar, including its overflow navigation.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	if a.state.ActiveSpace().Name != "Space 11" || a.spaceScroll.X == 0 {
		t.Fatalf("keyboard could not reach the last space: %s, scroll %v", a.state.ActiveSpace().Name, a.spaceScroll.X)
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.state.ActiveSpace().Name != "Personal" || a.spaceScroll.X != 0 {
		t.Fatal("switching with a shortcut did not reveal the active space")
	}
}

func TestSpaceBarAllSpacesMenuOnOverflow(t *testing.T) {
	a, tt := newTestApp(t)
	for i := 1; i < 12; i++ {
		a.state.Add(fmt.Sprintf("Space %d", i), t.TempDir())
	}
	tt.SetSize(640, 420)
	r, ok := tt.Find("全部空间")
	if !ok || r.X < 640-sidebarWidth || r.X+r.W > 640 {
		t.Fatalf("all-spaces picker missing or overflowed: found=%v rect=%+v", ok, r)
	}
	if err := tt.Click("全部空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Space 10"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.state.ActiveSpace().Name != "Space 10" {
		t.Fatalf("active space = %q, want Space 10", a.state.ActiveSpace().Name)
	}
}

func TestSpaceRestoresEditorAndTreeScroll(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.state.ActiveSpace()
	work := a.state.Add("Work", t.TempDir())
	for i := 0; i < 35; i++ {
		if err := vault.Write(first.Root, fmt.Sprintf("Note %02d.md", i), "# Note\n"); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshTree()
	tt.Frame()
	tt.Scroll(1080, 400, 0, 350)
	tt.Advance(time.Second)
	scrolled := a.session(first.ID).tree.Y
	if scrolled == 0 {
		t.Fatal("test did not scroll the file tree")
	}
	original := a.doc
	original.ed.ReplaceText(nil, "Saved ")
	original.ed.SetSelection(2, 5)
	tt.Key(ui.Cmd, ui.Key2)
	if a.state.ActiveSpace() != work || original.dirty() {
		t.Fatal("leaving the space did not save the edited document")
	}
	tt.Key(ui.Cmd, ui.Key1)
	anchor, caret := a.doc.ed.Positions()
	if a.doc != original || !a.doc.ed.CanUndo() || anchor != 2 || caret != 5 {
		t.Fatal("space switch discarded the editor's undo or selection")
	}
	if a.session(first.ID).tree.Y != scrolled {
		t.Fatal("space switch discarded the file tree's scroll position")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	source := a.doc.ed
	tt.Key(ui.Cmd, ui.Key2)
	tt.Key(ui.Cmd, ui.Key1)
	if !a.doc.source || a.doc.ed != source {
		t.Fatal("space switch discarded source mode")
	}
	tt.Key(ui.Cmd, ui.KeyBackslash)
	tt.Advance(time.Second)
	tt.Move(1197, 400)
	tt.Advance(time.Second)
	if a.session(first.ID).tree.Y != scrolled {
		t.Fatal("floating sidebar discarded the file tree's scroll position")
	}
}

func TestSpaceRestoreUsesChangedFile(t *testing.T) {
	a, tt := newTestApp(t)
	a.state.Favorites = nil // a favorite would stay open across the switch
	first := a.state.ActiveSpace()
	a.state.Add("Work", t.TempDir())
	original := a.doc
	tt.Key(ui.Cmd, ui.Key2)
	changed := "# Changed externally\n"
	if err := vault.Write(first.Root, "Welcome.md", changed); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc == original || a.doc.diskText != changed || a.doc.dirty() {
		t.Fatal("restored a stale editor over an externally changed file")
	}
	tt.Key(ui.Cmd, ui.Key2)
	if err := os.Remove(filepath.Join(first.Root, "Welcome.md")); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc != nil || first.LastOpen != "" {
		t.Fatal("restored an externally deleted document")
	}
}

func TestSpaceKeepsCollapsedFolders(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.state.ActiveSpace()
	a.state.Add("Work", t.TempDir())
	if err := tt.Click("Ideas"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Journal"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.Key2)
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc.rel != "Journal/Ideas.md" || first.IsExpanded("Journal") {
		t.Fatal("restoring the last document changed the space's collapsed folders")
	}
}

func TestSpaceSwitchStopsOnSaveFailure(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.state.ActiveSpace()
	a.state.Add("Work", t.TempDir())
	original, originalRoot, at := a.doc, first.Root, a.at
	original.ed.ReplaceText(nil, "Unsaved ")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("blocks directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	first.Root = filepath.Join(blocker, "space")
	tt.Key(ui.Cmd, ui.Key2)
	if a.state.ActiveSpace() != first || a.doc != original || !a.doc.dirty() || a.at != at || a.doc.saveErr == nil {
		t.Fatal("failed save discarded the document or changed the space")
	}
	first.Root = originalRoot
	tt.Key(ui.Cmd, ui.Key2)
	tt.Key(ui.Cmd, ui.Key1)
	if a.doc != original || a.doc.dirty() || a.doc.saveErr != nil {
		t.Fatal("retry did not save and restore the original editor")
	}
}

// Dragging the sidebar's inner edge sets its width, kept in the prefs and
// bounded; a double click goes back to the standard width.
func TestSidebarResizesByDraggingItsEdge(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1200, 800)
	settle(tt)
	edge, ok := tt.Find("调整侧边栏宽度")
	if !ok || edge.X < 1200-sidebarWidth-8 || edge.X > 1200-sidebarWidth+8 {
		t.Fatalf("the edge is not at the sidebar's inner side: %+v", edge)
	}
	x, y := edge.X+edge.W/2, edge.Y+edge.H/2
	tt.Press(x, y)
	tt.Move(x-40, y)
	tt.Frame()
	tt.Move(x-80, y)
	tt.Frame()
	tt.Release(x-80, y)
	settle(tt)
	if a.prefs.SidebarWidth != sidebarWidth+80 || a.sidebarW() != sidebarWidth+80 {
		t.Fatal("dragged width", a.prefs.SidebarWidth)
	}
	stored, err := a.prefStore.Load()
	if err != nil || stored.SidebarWidth != sidebarWidth+80 {
		t.Fatal("kept", stored, err)
	}
	a.prefs.SidebarWidth = 5000
	if a.sidebarW() != sidebarMax {
		t.Fatal("bounded", a.sidebarW())
	}
	a.prefs.SidebarWidth = 0
	if a.sidebarW() != sidebarWidth {
		t.Fatal("standard")
	}
}

// A double click on the sidebar's edge goes back to the standard width,
// and from the standard width out to the widest the window allows.
func TestSidebarEdgeDoubleClick(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1200, 800)
	a.prefs.SidebarWidth = sidebarWidth + 80
	settle(tt)
	double := func() {
		t.Helper()
		edge, ok := tt.Find("调整侧边栏宽度")
		if !ok {
			t.Fatal("no edge")
		}
		x, y := edge.X+edge.W/2, edge.Y+edge.H/2
		for range 2 {
			tt.Press(x, y)
			tt.Frame()
			tt.Release(x, y)
			tt.Frame()
		}
		settle(tt)
	}
	double()
	if a.sidebarW() != sidebarWidth || a.prefs.SidebarWidth != 0 {
		t.Fatal("standard width", a.sidebarW(), a.prefs.SidebarWidth)
	}
	double()
	if a.sidebarW() != sidebarMax {
		t.Fatal("widest", a.sidebarW())
	}
	if stored, err := a.prefStore.Load(); err != nil || stored.SidebarWidth != sidebarMax {
		t.Fatal("kept", stored, err)
	}
	double()
	if a.sidebarW() != sidebarWidth {
		t.Fatal("back to standard", a.sidebarW())
	}
}
