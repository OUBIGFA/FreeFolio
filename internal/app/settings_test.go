package app

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeMD/internal/prefs"
	"github.com/OUBIGFA/FreeMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestMoreMenuAndSettings(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("设置…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settingsOpen || a.settingsTab != tabGeneral || !tt.HasText("新空间的默认位置") {
		t.Fatalf("settings: open %v, tab %q", a.settingsOpen, a.settingsTab)
	}
	if err := tt.Click("快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("命令栏") || !tt.HasText(a.keyLabel("replace")) || !tt.HasText(a.keyLabel("back")) {
		t.Errorf("shortcuts tab: %q", tt.Texts())
	}
	if err := tt.Click("空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Personal") {
		t.Errorf("spaces tab: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.settingsOpen {
		t.Fatal("Escape kept the settings open")
	}

	// ⌘, opens them too; About remains available inside settings.
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("⌘, did not open the settings")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Click("更多")
	tt.Frame()
	if err := tt.Click("设置…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsTab != tabAbout || !tt.HasText("版本 "+Version) {
		t.Errorf("about: tab %q", a.settingsTab)
	}
	if err := tt.Click("GitHub 开源仓库"); err != nil {
		t.Fatal(err)
	}
	wantURL := "https://github.com/OUBIGFA/FreeMD"
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{wantURL}) {
		t.Fatalf("repository link: %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	if got := tt.OpenedURLs(); !slices.Equal(got, []string{wantURL, wantURL}) {
		t.Fatalf("repository link from keyboard: %q", got)
	}
}

// Export is one menu entry, with both formats reachable by pointer or keyboard.
func TestMoreMenuExport(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		t.Run(fmt.Sprintf("keyboard=%v", keyboard), func(t *testing.T) {
			a, tt := newTestApp(t)
			if err := tt.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("导出 Markdown 文件") || tt.HasText("导出 PDF") {
				t.Fatal("export formats should only appear in the submenu")
			}
			if keyboard {
				tt.Key(0, ui.KeyHome)
				for range 5 {
					tt.Key(0, ui.KeyDown)
				}
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click("导出"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("导出 Markdown 文件") || !tt.HasText("导出 PDF") {
				t.Fatal("export submenu did not show both formats")
			}
			if keyboard {
				tt.Key(0, ui.KeyEnter)
			} else if err := tt.Click("导出 Markdown 文件"); err != nil {
				t.Fatal(err)
			}
			got, err := vault.Read(root(a), "Welcome Export.md")
			if err != nil || got != a.doc.text() {
				t.Fatalf("exported Markdown differs from the document: %v", err)
			}
		})
	}
}

func TestMoreMenuWithoutUsableDocument(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("load-error=%v", failed), func(t *testing.T) {
			a, tt := newTestApp(t)
			if failed {
				a.doc.loadErr = errors.New("unreadable document")
			} else {
				a.doc = nil
			}
			tt.Frame()
			if err := tt.Click("更多"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("导出"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("导出 Markdown 文件") || tt.HasText("导出 PDF") {
				t.Fatal("export opened without a usable document")
			}
			if err := tt.Click("设置…"); err != nil {
				t.Fatal(err)
			}
			if !a.settingsOpen {
				t.Fatal("settings should remain reachable without a usable document")
			}
		})
	}
}

// TestSettingsSnapshot renders the ⋯ menu and each settings tab, for review.
func TestSettingsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to render the settings")
	for _, name := range []string{"more-menu", "more-menu@dark", "more-menu@narrow", "export-menu", tabGeneral, tabWriting, tabAI, tabUpdates, "writing@narrow", "ai@narrow", tabAppearance, tabSpaces, tabShortcuts, tabAbout, "about@dark", "about@narrow"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1240, 760)
			tab, variant, _ := strings.Cut(name, "@")
			if variant == "dark" {
				tt.SetDark(true)
			}
			if variant == "narrow" {
				tt.SetSize(640, 500)
			}
			settle(tt)
			if tab == "more-menu" || tab == "export-menu" {
				tt.Click("更多")
				if tab == "export-menu" {
					tt.Click("导出")
				}
			} else {
				a.openSettings(tab)
			}
			for range 4 {
				tt.Frame()
			}
			file, err := os.Create(filepath.Join(dir, fmt.Sprintf("settings-%s.png", name)))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// The word count and the format menu sit in the toolbar, beside the
// address, and leave with the editor in the export preview.
func TestToolbarCorner(t *testing.T) {
	a, tt := newTestApp(t)
	tt.Frame()
	r, ok := tt.Find("更多格式")
	if !ok || r.Y > 46 {
		t.Fatalf("format menu at %v, found %v", r, ok)
	}
	if _, ok := tt.Find("Markdown 源码"); !ok {
		t.Fatal("the source toggle is missing from the toolbar")
	}
	if err := tt.Click("更多格式"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "文字") || !slices.Contains(menu, "段落") || !slices.Contains(menu, "插入") || slices.Contains(menu, "撤销") {
		t.Fatalf("format menu: %q", tt.Menu())
	}
	tt.CloseMenu()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	tt.Frame()
	if !a.doc.source {
		t.Fatal("⇧⌘M did not open the export preview")
	}
	if _, ok := tt.Find("更多格式"); ok {
		t.Fatal("format menu shown over the export preview")
	}
}

func TestFixedSettingsFitMinimumWindow(t *testing.T) {
	for tab, labels := range map[string][]string{
		tabGeneral:    {"新空间的默认位置", "配置文件夹（spaces.json、插件）"},
		tabAppearance: {"界面语言", "正文字体", "页面宽度", "正文字号", "行距", "恢复默认外观"},
		tabWriting:    {"打字机滚动", "打开文档时画出手绘标注", "专注模式淡化其他段落", "括号与引号自动配对", "中英文之间自动加空格", "代码块自动换行"},
		tabSpaces:     {"新建空间…"}, tabUpdates: {"检查更新…"}, tabAbout: {"重新观看"}, tabAI: {"服务地址", "粘贴 API Key", "输入模型名称", "获取模型"},
	} {
		t.Run(tab, func(t *testing.T) {
			a, tt := newTestApp(t)
			if tab == tabAI {
				setupOneBot(a, "")
			}
			tt.SetSize(640, 420)
			a.openSettings(tab)
			tt.Frame()
			tt.Frame()
			for _, label := range labels {
				scrollIntoView(tt, label, 641, 421)
				r, ok := tt.Find(label)
				if !ok || r.W < 1 || r.H < 1 || r.Y < 0 || r.X < 0 || r.X+r.W > 641 || r.Y+r.H > 421 {
					t.Errorf("%q out of view: %+v found=%v", label, r, ok)
				}
			}
		})
	}
}

// scrollIntoView scrolls the settings page until what shows label is in a
// window w by h, as a reader scrolls to it.
func scrollIntoView(tt *ui.Tester, label string, w, h float32) {
	for range 20 {
		r, ok := tt.Find(label)
		if !ok || r.H >= 1 && r.Y >= 0 && r.Y+r.H <= h {
			return
		}
		tt.Scroll(w-40, h/2, 0, 120)
		tt.Frame()
	}
}

func TestFileBrowserSettings(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabFiles)
	tt.Frame()
	for _, label := range []string{"显示标签页关闭按钮", "双击标签页关闭", "以只读方式打开文件", "关闭最后一个标签页时关闭窗口"} {
		sw, ok := tt.AccessByRole(ui.RoleSwitch, label)
		if !ok {
			t.Fatal("no switch", label)
		}
		tt.ClickAt(sw.Bounds.X+sw.Bounds.W/2, sw.Bounds.Y+sw.Bounds.H/2)
		tt.Frame()
	}
	if err := tt.Click("当前标签页右侧"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	want := prefs.Files{KeepOpen: true, NoDoubleClickClose: true, HideTabClose: true, TabAfterCurrent: true, ReadOnly: true}
	saved, err := (prefs.Store{Path: a.prefStore.Path}).Load()
	if err != nil || a.prefs.Files != want || saved.Files != want {
		t.Fatalf("file browser choices: %+v, saved %+v, %v", a.prefs.Files, saved, err)
	}
}

// Closing to the tray is on by default and the general page turns it off.
func TestCloseToTraySetting(t *testing.T) {
	a, tt := newTestApp(t)
	if a.prefs.KeepRunningOff {
		t.Fatal("on by default")
	}
	a.openSettings(tabGeneral)
	tt.Frame()
	sw, ok := tt.AccessByRole(ui.RoleSwitch, "关闭窗口时最小化到托盘")
	if !ok || !sw.Checked {
		t.Fatal("the switch shows on", sw)
	}
	tt.ClickAt(sw.Bounds.X+sw.Bounds.W/2, sw.Bounds.Y+sw.Bounds.H/2)
	tt.Frame()
	stored, err := a.prefStore.Load()
	if !a.prefs.KeepRunningOff || err != nil || !stored.KeepRunningOff {
		t.Fatal("turned off and kept", stored, err)
	}
}
