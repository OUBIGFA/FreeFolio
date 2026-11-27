package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/mdz"
	"github.com/OUBIGFA/FreeFolio/internal/vault"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func TestMDZipIncludesWhiteboardMediaAndHistory(t *testing.T) {
	a, _ := newTestApp(t)
	sp := a.state.ActiveSpace()
	if err := vault.Write(sp.Root, "boards/shared/result.png", "image bytes"); err != nil {
		t.Fatal(err)
	}
	scene := whiteboard.NewScene()
	node := whiteboard.NewAINode(whiteboard.AIImage, 0, 0)
	data := whiteboard.AINodeOf(node)
	data.Outputs = []whiteboard.AIOutput{{Kind: "image", Path: "shared/result.png"}}
	if err := whiteboard.SetAINode(node, data); err != nil {
		t.Fatal(err)
	}
	scene.Elements = append(scene.Elements, node)
	scene.Files["image"] = &whiteboard.FileData{ID: "image", MIMEType: "image/png", Path: "shared/result.png"}
	board, err := encodeBoard("boards/Diagram.excalidraw.svg", scene)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "boards/Diagram.excalidraw.svg", board); err != nil {
		t.Fatal(err)
	}
	if err := vault.Write(sp.Root, "Bundle.md", "# Bundle\n\n![Board](boards/Diagram.excalidraw.svg)\n"); err != nil {
		t.Fatal(err)
	}
	a.open("Bundle.md")
	a.exportMDZ()
	archive, err := mdz.Open(filepath.Join(sp.Root, "Bundle.mdz"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	packed, err := archive.ReadFile("boards/Diagram.excalidraw.svg")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := whiteboard.Decode(string(packed))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{loaded.Files["image"].Path, whiteboard.AINodeOf(loaded.Elements[0]).Outputs[0].Path} {
		resolved := archive.ResolveLink("boards/Diagram.excalidraw.svg", ref)
		if !resolved.Found {
			t.Fatalf("board media not bundled: %q", ref)
		}
		content, err := archive.ReadFile(resolved.Path)
		if err != nil || string(content) != "image bytes" {
			t.Fatal("media bytes changed")
		}
	}
	original, _ := vault.Read(sp.Root, "boards/Diagram.excalidraw.svg")
	if original != board {
		t.Fatal("export changed original board")
	}
}

func TestFileBrowserMissingCleanupProtectsUnsavedEditor(t *testing.T) {
	a, _ := newTestApp(t)
	first := externalFile(t, "Gone.md", "# Gone\n")
	if !a.openSystemFile(first) {
		t.Fatal("open")
	}
	d := a.doc
	a.prefs.NoAutosave = true
	d.ed.SetSelection(d.ed.Len(), d.ed.Len())
	d.ed.ReplaceText(nil, "keep me")
	// Rename simulates an external move without deleting a user's file.
	if err := os.Rename(first, first+".moved"); err != nil {
		t.Fatal(err)
	}
	a.cleanMissingFileTabs()
	if a.doc != d || len(a.files.order) != 1 {
		t.Fatal("cleanup dropped unsaved text")
	}
}
