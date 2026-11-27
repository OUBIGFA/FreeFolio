package app

import (
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/localresource"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

func TestLoRAResourceTabsFilterByBaseModel(t *testing.T) {
	a, tt := newTestApp(t)
	a.openSettings(tabResources)
	a.resources.tab = 3
	tt.Frame()
	for _, tc := range []struct {
		tab, visible, hidden string
		index                int
	}{
		{"Qwen 2.1 Turbo", "Qwen 2.1 Character Swap", "Z-Image Detailer", 1},
		{"Qwen 2.1 普通版", "FUSAL Anime V3", "Qwen 2.1 Character Swap", 2},
		{"Z-Image Turbo", "Z-Image Detailer", "FUSAL Anime V3", 0},
	} {
		found := false
		for _, n := range tt.AccessAll() {
			if n.Role == ui.RoleTab && n.Name == tc.tab {
				tt.ClickAt(n.Bounds.X+n.Bounds.W/2, n.Bounds.Y+n.Bounds.H/2)
				found = true
				break
			}
		}
		tt.Frame()
		if !found || a.resources.loraBaseTab != tc.index || !tt.HasText(tc.visible) || tt.HasText(tc.hidden) {
			t.Fatalf("wrong LoRA base tab: %s", tc.tab)
		}
		if tt.HasText("Qwen 2.1 Detail Enhancer") || a.resources.busy != "" {
			t.Fatal("removed adapter shown or tab starts downloads")
		}
	}
	for _, preset := range a.boardEnhancementPresets() {
		if preset.Model.Model == "qwen-character-swap" && preset.Role != whiteboard.AIImage {
			t.Fatal("two-image adapter uses one-image enhancement")
		}
		if preset.Model.Model == "qwen-detail" {
			t.Fatal("removed adapter still offered as a preset")
		}
	}
	models := a.boardAIModels(whiteboard.AIModel)
	found := false
	for _, model := range models {
		if model.Model == "qwen-image-2.1-int8" {
			found = true
		}
	}
	if !found {
		t.Fatal("ordinary base missing from model loader")
	}
	pack, _ := localresource.ComfyPack("qwen-fusal-turbo")
	download, _ := localresource.PackDownload(pack)
	a.setPackDownloadProgress(pack, download)
	if a.resources.total != 671174216 || a.resources.dependencyLabel != "依赖下载" {
		t.Fatal("extra encoder is counted as LoRA weight")
	}
}

func TestQwenLoRATabsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect Qwen base tabs")
	a, tt := newTestApp(t)
	tt.SetSize(1400, 1000)
	a.openSettings(tabResources)
	a.resources.tab = 3
	for i, name := range []string{"z-image", "qwen-turbo", "qwen-base"} {
		a.resources.loraBaseTab = i
		for range 3 {
			tt.Frame()
		}
		savePNG(t, filepath.Join(dir, name+"-loras.png"), tt)
	}
}
