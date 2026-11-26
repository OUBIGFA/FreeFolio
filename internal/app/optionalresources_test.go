package app

import (
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/localresource"
)

func TestOptionalResourcesChooseDirectoryAndDoNotAutoDownload(t *testing.T) {
	a, tester := newTestApp(t)
	a.openSettings(tabResources)
	tester.Frame()
	if !tester.HasText("BiRefNet Lite") || !tester.HasText("BiRefNet General") {
		t.Fatal("missing cutout choices")
	}
	root := t.TempDir()
	a.setResourceDirectory(root)
	tester.Frame()
	saved, err := a.prefStore.Load()
	if err != nil || saved.ResourceDir != root || a.resourceDirectory() != root {
		t.Fatalf("resource directory not saved: %v %v", saved.ResourceDir, err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("choosing a folder downloaded files: %v", files)
	}
	for _, item := range localresource.Models {
		if _, ok := tester.Access("下载 " + item.Name); !ok {
			t.Fatalf("missing download action for %s", item.Name)
		}
	}
}

func TestSettingsTabsHaveIcons(t *testing.T) {
	for _, tab := range settingsTabs() {
		if settingsIcons[tab.icon] == nil {
			t.Fatalf("settings tab %q (%s) is missing icon %q", tab.id, tab.label, tab.icon)
		}
	}
}
