package app

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func externalFile(t *testing.T, name, content string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestLooseFilesStayOutOfSearchAndLibrary(t *testing.T) {
	a, tt := newTestApp(t)
	first := externalFile(t, "README.md", "# First external\\n\\nOpened unique content")
	second := externalFile(t, "README.md", "# Second external")
	if err := os.WriteFile(filepath.Join(filepath.Dir(first), "Unopened.md"), []byte("secret sibling"), 0600); err != nil {
		t.Fatal(err)
	}
	count := len(a.state.Spaces)
	for _, file := range []string{first, second, first} {
		if !a.openSystemFile(file) {
			t.Fatal("open", file)
		}
	}
	tt.Frame()
	if a.doc.fullPath() != first || len(a.files.order) != 2 || a.state.Get(space.ExternalID) != nil || len(a.state.Spaces) != count {
		t.Fatal("loose paths entered the workspace or collided")
	}
	if !tt.HasText("README.md · "+filepath.Base(filepath.Dir(first))) || tt.HasText("Unopened.md") || tt.HasText(filepath.Dir(first)) {
		t.Fatal("incorrect file view")
	}
	if _, err := resolveFile(a.doc.space, "Unopened.md"); err == nil {
		t.Fatal("sibling resolved")
	}
	a.refreshSearch()
	if len(a.textHits("unique content")) != 0 || len(a.textHits("secret sibling")) != 0 {
		t.Fatal("search indexed a loose file")
	}
	a.loadLibrary()
	if _, ok := a.library.trees[space.ExternalID]; ok {
		t.Fatal("library contains loose documents")
	}
	restarted, err := New(a.cfg)
	// A launch picks up the space's last document, never the file browser.
	if err != nil || restarted.files != nil || restarted.doc == nil || restarted.doc.space.IsExternal() {
		t.Fatal("restart restored a loose file", err)
	}
}

func TestExternalInPlaceSaveAndRename(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "External.md", "# Original\n\nBody\n")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	sp := a.doc.space

	a.doc.ed.ReplaceText(nil, "Edited ")
	if !a.flush() {
		t.Fatal("save")
	}
	if a.doc.fullPath() != file || len(sp.ExternalFiles) != 1 {
		t.Fatal("in-place save lost identity")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), "External.arcmd")); !os.IsNotExist(err) {
		t.Fatal("external Markdown save created a .arcmd file")
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), "Edited") {
		t.Fatalf("external file %s: %v", b, err)
	}
	a.renameIn(sp, a.doc.rel)
	a.newName = "Renamed"
	a.commitRename()
	if a.doc.fullPath() != filepath.Join(filepath.Dir(file), "Renamed.md") || len(sp.ExternalFiles) != 1 {
		t.Fatal("rename lost file")
	}
	a.exportMarkdown()
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), "Renamed Export.md")); err != nil {
		t.Fatal(err)
	}
}

func TestExternalLinkedFileOpensOnlyWhenFollowed(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "Start.md", "[Next](Next.md)")
	next := filepath.Join(filepath.Dir(file), "Next.md")
	if err := os.WriteFile(next, []byte("# Next"), 0600); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(file) || len(a.files.order) != 1 {
		t.Fatal("opened siblings automatically")
	}
	a.followLink("Next.md")
	if a.doc.fullPath() != next || len(a.files.order) != 2 {
		t.Fatal("linked file did not open in collection")
	}
}

func TestExternalRenameSavesDirtyMarkdown(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "External.md", "# Original")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	a.doc.ed.ReplaceText(nil, "Edited ")
	a.renameIn(a.doc.space, a.doc.rel)
	a.newName = "Renamed"
	a.commitRename()
	if a.doc.fullPath() != filepath.Join(filepath.Dir(file), "Renamed.md") || len(a.doc.space.ExternalFiles) != 1 {
		t.Fatal("rename did not follow the saved document")
	}
	if data, err := os.ReadFile(a.doc.fullPath()); err != nil || !strings.Contains(string(data), "Edited") {
		t.Fatal("renamed file lost changes", err)
	}
}

// Closing the last tab closes the window; only "返回空间" returns to the
// spaces, from the kept empty browser too.
func TestClosingLastTabClosesWindowNotReturns(t *testing.T) {
	a, tt := newTestApp(t)
	original := a.doc
	closed := 0
	a.closeMain = func() { closed++ }
	file := externalFile(t, "External.md", "# Original")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	tt.Frame()
	if err := tt.Click("关闭标签页 External.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if closed != 1 || a.files == nil || a.doc != nil {
		t.Fatal("the last tab did not close the window", closed)
	}
	// Close lands frames later; until then nothing unrelated may flash.
	if tt.HasText("没有打开的文件") || tt.HasText("返回空间") {
		t.Fatal("closing the last tab drew the empty file browser")
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "# Original" {
		t.Fatal("close changed original", err)
	}

	a.prefs.Files.KeepOpen = true
	a.openSystemFile(file)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyW)
	tt.Frame()
	if closed != 1 || a.files == nil || !tt.HasText("没有打开的文件") {
		t.Fatal("the kept browser did not stay empty")
	}
	if err := tt.Click("返回空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.files != nil || a.doc != original {
		t.Fatal("did not restore the workspace editor")
	}
}

func TestFileTabsSwitchAndClose(t *testing.T) {
	a, tt := newTestApp(t)
	a.closeMain = func() { t.Fatal("closed the window with tabs left") }
	files := []string{externalFile(t, "One.md", "# One"), externalFile(t, "Two.md", "# Two"), externalFile(t, "Three.md", "# Three")}
	for _, file := range files {
		a.openSystemFile(file)
	}
	tt.Frame()
	if a.doc.fullPath() != files[2] {
		t.Fatal("the last opened file is not shown")
	}
	tab := func(name string) ui.Rect {
		t.Helper()
		r, ok := tt.Find(name)
		if !ok {
			t.Fatal("no tab", name)
		}
		return r
	}
	if err := tt.Click("One.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.fullPath() != files[0] {
		t.Fatal("a click did not show its tab")
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Frame()
	if a.doc.fullPath() != files[1] {
		t.Fatal("Ctrl+Tab did not show the next tab")
	}

	// A double click closes a tab, unless turned off.
	a.prefs.Files.NoDoubleClickClose = true
	r := tab("Three.md")
	tt.ClickAt(r.X+12, r.Y+r.H/2)
	tt.ClickAt(r.X+12, r.Y+r.H/2)
	tt.Frame()
	if len(a.files.order) != 3 || a.doc.fullPath() != files[2] {
		t.Fatal("a double click closed a tab while turned off")
	}
	a.prefs.Files.NoDoubleClickClose = false
	tt.Advance(time.Second)
	tt.ClickAt(r.X+12, r.Y+r.H/2)
	tt.ClickAt(r.X+12, r.Y+r.H/2)
	tt.Frame()
	if len(a.files.order) != 2 || a.doc.fullPath() != files[1] {
		t.Fatal("a double click did not close the tab and show its neighbor", a.files.order)
	}

	// The middle button closes a tab that is not shown, leaving the shown one.
	r = tab("One.md")
	tt.PressButton(2, r.X+12, r.Y+r.H/2)
	tt.Frame()
	tt.ReleaseButton(2, r.X+12, r.Y+r.H/2)
	tt.Frame()
	if len(a.files.order) != 1 || a.doc.fullPath() != files[1] {
		t.Fatal("the middle button did not close the tab", a.files.order)
	}

	a.prefs.Files.HideTabClose = true
	tt.Frame()
	if _, ok := tt.Find("关闭标签页 Two.md"); ok {
		t.Fatal("the close button shows while hidden")
	}
}

func TestFileTabsCenterAsAGroup(t *testing.T) {
	for _, profile := range []struct {
		name string
		bar  ui.TitleBar
	}{
		{"windows", windowsTitleBar},
		{"macos", ui.TitleBar{Height: 46, Left: 78}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetTitleBar(profile.bar)
			tt.SetSize(1440, 760)
			for i := range 9 {
				name := fmt.Sprintf("Note-%d.md", i+1)
				if !a.openSystemFile(externalFile(t, name, "# Note")) {
					t.Fatal("open", name)
				}
				settle(tt)
				strip, ok := tt.Find("已打开的文件")
				if !ok || strip.W <= 0 {
					t.Fatalf("%d tabs: missing tab strip", i+1)
				}
				first, _ := tt.Find("Note-1.md")
				last, found := tt.Find(name)
				if !found || last.X < strip.X-1 || last.X+last.W > strip.X+strip.W+1 {
					t.Errorf("%d tabs: active tab %+v is outside strip %+v", i+1, last, strip)
				}
				// Short groups center their combined bounds, not each tab separately.
				actions, _ := tt.Find("添加分屏")
				openButton, _ := tt.Find("打开文件")
				groupWidth := last.X + last.W - first.X
				canCenter := 720+groupWidth/2+6+openButton.W <= actions.X-12
				if i < 3 && canCenter && math.Abs(float64((first.X+last.X+last.W)/2-720)) > 1 {
					t.Errorf("%d tabs: group is not centered: first=%+v last=%+v", i+1, first, last)
				}
				if i == 8 {
					word, hasWord := tt.Find("1 字")
					if !hasWord || strip.X < word.X+word.W || strip.X > word.X+word.W+24 {
						t.Errorf("crowded tabs did not align left after brand word count: word=%+v strip=%+v", word, strip)
					}
				}
				open, ok := tt.Find("打开文件")
				if !ok || open.X < strip.X+strip.W || open.X > strip.X+strip.W+12 {
					t.Errorf("%d tabs: open button is not beside the strip's right edge: strip=%+v open=%+v", i+1, strip, open)
				}
			}
			a.showFileTab(a.files.order[0])
			settle(tt)
			first, _ := tt.Find("Note-1.md")
			strip, _ := tt.Find("已打开的文件")
			if first.X < strip.X-1 || first.X+first.W > strip.X+strip.W+1 {
				t.Fatalf("switching to first tab did not reveal it: tab=%+v strip=%+v", first, strip)
			}
			a.showFileTab(a.files.order[len(a.files.order)-1])
			for _, width := range []int{1080, 640, 1440} {
				tt.SetSize(width, 760)
				settle(tt)
				strip, _ = tt.Find("已打开的文件")
				last, _ := tt.Find("Note-9.md")
				if last.X+last.W > strip.X+strip.W+1 || last.X+last.W <= strip.X {
					t.Errorf("width=%d: active tab is not visible: tab=%+v strip=%+v", width, last, strip)
				}
				open, _ := tt.Find("打开文件")
				actions, _ := tt.Find("添加分屏")
				if strip.X < profile.bar.Left || open.X < strip.X+strip.W || open.X+open.W > actions.X-11 {
					t.Errorf("width=%d: strip %+v overlaps controls: open=%+v actions=%+v", width, strip, open, actions)
				}
			}
			a.closeOtherFileTabs(a.doc.rel, func(int, int) bool { return true })
			settle(tt)
			last, _ := tt.Find("Note-9.md")
			if math.Abs(float64(last.X+last.W/2-720)) > 1 {
				t.Errorf("closing other tabs did not recenter the remaining tab: %+v", last)
			}
			tt.SetSize(640, 760)
			settle(tt)
			last, _ = tt.Find("Note-9.md")
			word, hasWord := tt.Find("1 字")
			if !hasWord || word.X < profile.bar.Left+24 || last.X < word.X+word.W+11 || last.X > word.X+word.W+24 {
				t.Errorf("narrow window did not align its single tab left after brand word count: word=%+v tab=%+v", word, last)
			}
		})
	}
}

// A file of a space opened in the browser stays in it, and new tabs follow
// the chosen position.
func TestFileBrowserKeepsSpaceFilesAndPlacesTabs(t *testing.T) {
	a, tt := newTestApp(t)
	a.prefs.Files.TabAfterCurrent = true
	first, last := externalFile(t, "First.md", "# First"), externalFile(t, "Last.md", "# Last")
	a.openSystemFile(first)
	a.openSystemFile(last)
	a.showFileTab(a.files.order[0])
	inSpace := filepath.Join(root(a), "Inside.md")
	if err := os.WriteFile(inSpace, []byte("# Inside"), 0600); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(inSpace) || a.files == nil || a.doc.fullPath() != inSpace {
		t.Fatal("a file of a space left the browser")
	}
	if names := a.files.order; len(names) != 3 || names[1] != a.doc.rel {
		t.Fatal("the new tab is not right of the current one", names)
	}
	tt.Frame()
}

func TestFileBrowserReadOnlyIsPerTab(t *testing.T) {
	a, _ := newTestApp(t)
	a.prefs.Files.ReadOnly = true
	one, two := externalFile(t, "One.md", "# One"), externalFile(t, "Two.md", "# Two")
	a.openSystemFile(one)
	if !a.doc.ed.ReadOnly {
		t.Fatal("did not open read-only")
	}
	a.doc.ed.SetReadOnly(false)
	a.openSystemFile(two)
	if !a.doc.ed.ReadOnly {
		t.Fatal("the second file did not open read-only")
	}
	a.showFileTab(a.files.order[0])
	if a.doc.ed.ReadOnly {
		t.Fatal("the unlocked tab locked again")
	}
}

func TestExternalRemoveSavesDirtyMarkdown(t *testing.T) {
	a, _ := newTestApp(t)
	file := externalFile(t, "External.md", "# Original")
	a.openSystemFile(file)
	sp, rel := a.doc.space, a.doc.rel
	a.doc.ed.ReplaceText(nil, "Saved ")
	a.forgetExternal(sp, rel)
	if a.files == nil || len(a.files.order) != 0 || len(sp.ExternalFiles) != 0 {
		t.Fatal("forget left an entry open")
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), "Saved") {
		t.Fatal("edits lost", err)
	}
}

func TestExternalMoveToFolderSpaceKeepsDocument(t *testing.T) {
	a, _ := newTestApp(t)
	dst := a.state.ActiveSpace()
	file := externalFile(t, "External.md", "# Original")
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	sp, rel := a.doc.space, a.doc.rel
	a.doc.ed.ReplaceText(nil, "Edited ")
	to := a.moveAcross(sp.ID, rel, dst.ID, "")
	if to != "External.md" || a.doc.space != dst || len(sp.ExternalFiles) != 0 {
		t.Fatal("failed to move edited external document")
	}
	if !strings.Contains(a.doc.text(), "Edited") {
		t.Fatal("lost edits")
	}
	if data, err := os.ReadFile(filepath.Join(dst.Root, "External.md")); err != nil || !strings.Contains(string(data), "Edited") {
		t.Fatal("moved file missing edits", err)
	}
	if a.moveAcross(dst.ID, to, sp.ID, "") != "" {
		t.Fatal("collection accepted a physical move")
	}
}

func TestExternalWatchAndConflict(t *testing.T) {
	a, tt, w := watchApp(t)
	file := externalFile(t, "External.md", "")
	if err := writeNative(filepath.Dir(file), filepath.Base(file), "# Original"); err != nil {
		t.Fatal(err)
	}
	if !a.openSystemFile(file) {
		t.Fatal("open")
	}
	tt.Frame()
	changed := w.changed[file]
	if changed == nil || w.changed[filepath.Dir(file)] != nil {
		t.Fatal("must watch file, not parent directory")
	}
	if err := writeNative(filepath.Dir(file), filepath.Base(file), "# Changed elsewhere"); err != nil {
		t.Fatal(err)
	}
	changed([]mygo.FileEvent{{Op: mygo.FileWrite, Path: filepath.Base(file)}})
	if !strings.Contains(a.doc.text(), "Changed elsewhere") {
		t.Fatal("did not reload")
	}
	a.doc.ed.ReplaceText(nil, "Mine ")
	if err := writeNative(filepath.Dir(file), filepath.Base(file), "# New disk version"); err != nil {
		t.Fatal(err)
	}
	changed([]mygo.FileEvent{{Op: mygo.FileWrite, Path: filepath.Base(file)}})
	if !a.doc.conflict {
		t.Fatal("conflict not detected")
	}
	sp, rel := a.doc.space, a.doc.rel
	a.forgetExternal(sp, rel)
	if a.doc == nil || len(sp.ExternalFiles) != 1 {
		t.Fatal("conflict bypassed on close")
	}
	a.useDisk()
	if !strings.Contains(a.doc.text(), "New disk version") {
		t.Fatal("disk version not used")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	changed([]mygo.FileEvent{{Op: mygo.FileRemove, Path: filepath.Base(file)}})
	if a.doc != nil || len(sp.ExternalFiles) != 1 {
		t.Fatal("missing entry not retained")
	}
	a.forgetExternal(sp, rel)
	if len(sp.ExternalFiles) != 0 {
		t.Fatal("cannot remove missing entry")
	}
}

func TestExternalSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT for external space")
	for _, size := range []struct {
		name string
		w, h int
		dark bool
	}{{"external", 1240, 800, false}, {"external-dark", 1240, 800, true}, {"external-narrow", 760, 600, false}, {"external-empty", 1240, 800, false}} {
		t.Run(size.name, func(t *testing.T) {
			a, tt := newTestApp(t)
			file := externalFile(t, "旅行计划.md", "# 旅行计划\n\n从任何文件夹打开的文档，都可以在这里继续阅读和编辑。\n\n## 周末安排\n\n- 沿着海边散步\n- 找一家安静的咖啡馆\n")
			a.openSystemFile(file)
			a.openSystemFile(externalFile(t, "README.md", "# 项目说明\n\n一份来自下载文件夹的文档。"))
			a.openSystemFile(file)
			if size.name == "external-empty" {
				sp := a.doc.space
				for rel := range sp.ExternalFiles {
					a.forgetExternal(sp, rel)
				}
			}
			tt.SetSize(size.w, size.h)
			tt.SetDark(size.dark)
			if size.dark {
				th := space.ThemeOf(a.state.ActiveSpace())
				th.Appearance = space.AppearanceDark
				a.state.ActiveSpace().SetTheme(th)
			}
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			tt.Frame()
			savePNG(t, filepath.Join(dir, size.name+".png"), tt)
		})
	}
}

// The pinned taskbar button and the tray icon bring the window to the
// space's home page, out of the file browser and the open document.
func TestShowSpaceHomeLeavesFilesAndDocument(t *testing.T) {
	a, tt := newTestApp(t)
	a.open("Welcome.md")
	if !a.openSystemFile(externalFile(t, "Loose.md", "# Loose\n")) || a.files == nil {
		t.Fatal("open loose file")
	}
	tt.Frame()
	if !a.showSpaceHome() || a.files != nil || a.doc != nil {
		t.Fatalf("home: files=%v doc=%v", a.files, a.doc)
	}
	tt.Frame()
	if !tt.HasText("新建文档") {
		t.Fatal("no home page", tt.Texts())
	}
	if sp := a.state.ActiveSpace(); sp.LastOpen != "Welcome.md" {
		t.Fatal("the space forgot its last document", sp.LastOpen)
	}
}

// The file browser switches its page to Markdown source in place, as a
// space does: read-only while the tab is locked, editable once unlocked.
func TestFileBrowserSourceModeInPlace(t *testing.T) {
	a, tt := newTestApp(t)
	a.prefs.Files.ReadOnly = true
	file := externalFile(t, "One.md", "# One")
	a.openSystemFile(file)
	tt.Frame()
	if err := tt.Click("Markdown 源码"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("Markdown 源码编辑区"); !a.doc.source || !ok || tt.HasText("Markdown 导出预览") {
		t.Fatal("file browser did not switch to source in place")
	}
	a.doc.ed.SetReadOnly(false)
	a.doc.preview = "# One\n\nTyped in source.\n"
	tt.Frame()
	tt.Advance(sourceApplyDelay + time.Millisecond)
	tt.Advance(autosaveDelay + time.Millisecond)
	if b, _ := os.ReadFile(file); string(b) != "# One\n\nTyped in source.\n" {
		t.Fatalf("source edit not saved: %q", b)
	}
	if err := tt.Click("Markdown 源码"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.doc.source || !strings.Contains(a.doc.ed.Text(), "Typed in source.") {
		t.Fatal("did not return to rich text with the edit")
	}
}

// With room to spare, a long file name shows whole rather than cut at the
// usual tab width; crowded tabs keep that width and the strip scrolls.
func TestFileTabsShowLongNamesWhole(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetSize(1440, 760)
	long := "2026 年度项目复盘与下一阶段工作计划（最终修订版）.md"
	if !a.openSystemFile(externalFile(t, long, "# Note")) || !a.openSystemFile(externalFile(t, "短.md", "# Note")) {
		t.Fatal("open")
	}
	settle(tt)
	tab, ok := tt.Find(long)
	if !ok || tab.W <= fileTabCapWidth {
		t.Fatalf("long name cut although there is room: %+v", tab)
	}
	if w, _ := tt.Find("短.md"); w.W >= fileTabCapWidth {
		t.Fatalf("short name grew: %+v", w)
	}
	for i := range 8 {
		if !a.openSystemFile(externalFile(t, fmt.Sprintf("%d-%s", i, long), "# Note")) {
			t.Fatal("open", i)
		}
	}
	settle(tt)
	if tab, _ = tt.Find("7-" + long); math.Abs(float64(tab.W-fileTabCapWidth)) > 1 {
		t.Fatalf("crowded tab width = %v, want %v", tab.W, fileTabCapWidth)
	}
}
