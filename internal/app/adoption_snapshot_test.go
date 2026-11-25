package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/history"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestQuietAdoptionSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect adopted interactions")
	for _, name := range []string{"fonts", "fonts-ui", "history", "import", "cleanup", "quick-settings"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(1280, 820)
			switch name {
			case "fonts":
				a.openSettings(tabAppearance)
				a.fonts.loaded = true
				a.fonts.families = []ui.FontFamily{{Name: "Arial"}, {Name: "Georgia"}, {Name: "Microsoft YaHei"}, {Name: "SimSun"}, {Name: "KaiTi"}, {Name: "Consolas", Monospaced: true}}
				a.openFontPicker(false)
			case "fonts-ui":
				a.openSettings(tabAppearance)
				a.fonts.loaded = true
				a.fonts.families = []ui.FontFamily{{Name: "Arial"}, {Name: "Georgia"}, {Name: "Microsoft YaHei"}, {Name: "SimSun"}, {Name: "KaiTi"}, {Name: "Consolas", Monospaced: true}}
				a.openFontPicker(true)
			case "history":
				if e := vault.Write(root(a), "Preview.md", "# 当前稿\n\n当前内容\n"); e != nil {
					t.Fatal(e)
				}
				if e := history.Record(root(a), "Preview.md", "# 设计草稿\n\n一段 **重要内容**。\n\n| 项目 | 状态 |\n| --- | --- |\n| 字体预览 | 完成 |\n| 笔记迁移 | 验证中 |\n", time.Now().Add(-time.Hour), true); e != nil {
					t.Fatal(e)
				}
				a.refreshTree()
				a.open("Preview.md")
				a.openVersions()
			case "import":
				a.imports.open = true
				a.imports.spaceID = a.state.ActiveSpace().ID
			case "cleanup":
				if e := os.MkdirAll(filepath.Join(root(a), "assets"), 0755); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(root(a), "assets", "unused-image.png"), []byte("image"), 0600); e != nil {
					t.Fatal(e)
				}
				a.openSettings(tabSpaces)
				a.scanUnusedAssets()
			case "quick-settings":
				a.openSettings(tabGeneral)
			}
			tt.Frame()
			tt.Frame()
			tt.Advance(300 * time.Millisecond)
			savePNG(t, filepath.Join(dir, "adoption-"+name+".png"), tt)
		})
	}
}
