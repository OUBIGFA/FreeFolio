package app

import "testing"

func TestReopenWindowLeavesEmptyFileBrowser(t *testing.T) {
	for _, keepOpen := range []bool{false, true} {
		t.Run(map[bool]string{false: "close-last", true: "keep-empty"}[keepOpen], func(t *testing.T) {
			a, _ := newTestApp(t)
			a.addSplitPane()
			previous, split := a.doc, a.split
			a.prefs.Files.KeepOpen = keepOpen
			closed := 0
			a.closeMain = func() { closed++ }
			if !a.openSystemFile(externalFile(t, "Loose.md", "# Loose\n")) {
				t.Fatal("open")
			}
			a.closeFileTab(a.fileTabShown())
			if a.files == nil || len(a.files.order) != 0 {
				t.Fatal("test did not retain an empty browser")
			}
			if !keepOpen && closed != 1 {
				t.Fatal("last tab did not request window close")
			}
			a.prepareWindowReopen()
			if a.files != nil || a.doc != previous || a.split != split {
				t.Fatal("empty browser returned instead of suspended space")
			}
		})
	}
}

func TestReopenWindowRetainsOpenFileTabs(t *testing.T) {
	a, _ := newTestApp(t)
	if !a.openSystemFile(externalFile(t, "Still open.md", "# Keep\n")) {
		t.Fatal("open")
	}
	files, doc := a.files, a.doc
	a.prepareWindowReopen()
	if a.files != files || a.doc != doc || len(a.files.order) != 1 {
		t.Fatal("reopening discarded live file tabs")
	}
}

func TestReopenWindowStillHonorsSpaceHome(t *testing.T) {
	a, _ := newTestApp(t)
	a.prefs.StartAtHome = true
	a.prefs.Files.KeepOpen = true
	if !a.openSystemFile(externalFile(t, "Loose.md", "# Loose\n")) {
		t.Fatal("open")
	}
	a.closeFileTab(a.fileTabShown())
	a.prepareWindowReopen()
	if a.files != nil || a.doc != nil {
		t.Fatal("space home preference ignored")
	}
}
