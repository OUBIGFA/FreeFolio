package assetcleanup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func TestScanKeepsHistoryTrashLooseAndLiveReferences(t *testing.T) {
	root, backup := t.TempDir(), t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, n := range []string{"history.png", "trash.png", "loose.png", "live.png", "中文 图.png", "unused.png"} {
		write(filepath.Join(root, "assets", n), "image")
	}
	write(filepath.Join(root, ".freefolio/history/note.md/old.md"), "![](assets/history.png)")
	write(filepath.Join(backup, "item"), `{"image":"assets/trash.png"}`)
	loose := filepath.Join(t.TempDir(), "loose.md")
	write(loose, "![](assets/loose.png)")
	write(filepath.Join(root, "note.md"), "![](assets/%E4%B8%AD%E6%96%87%20%E5%9B%BE.png)")
	items, e := Scan(Request{Root: root, ReferenceRoots: []string{backup}, ReferenceFiles: []string{loose}, Live: []string{"![](assets/live.png)"}})
	if e != nil || len(items) != 1 || items[0].Path != "assets/unused.png" {
		t.Fatalf("unsafe candidates: %+v %v", items, e)
	}
	write(filepath.Join(root, "later.md"), "![](assets/unused.png)")
	items, e = Scan(Request{Root: root, ReferenceRoots: []string{backup}, ReferenceFiles: []string{loose}, Live: []string{"![](assets/live.png)"}})
	if e != nil || len(items) != 0 {
		t.Fatalf("fresh reference missed: %+v %v", items, e)
	}
}

func TestScanReadsAttachmentReferencesInsideBoardSVGPayload(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "assets"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "assets", "ai-output.png"), []byte("image"), 0600); e != nil {
		t.Fatal(e)
	}
	scene := whiteboard.NewScene()
	scene.Extra = map[string]json.RawMessage{"attachment": json.RawMessage(`"assets/ai-output.png"`)}
	raw, e := scene.Encode()
	if e != nil {
		t.Fatal(e)
	}
	payload, e := whiteboard.EncodeSVGPayload(raw)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "board.excalidraw.svg"), []byte("<svg>"+payload+"</svg>"), 0600); e != nil {
		t.Fatal(e)
	}
	items, e := Scan(Request{Root: root})
	if e != nil || len(items) != 0 {
		t.Fatalf("board references missed: %+v %v", items, e)
	}
}

func TestScanCustomAssetsAndRejectsEscapingFolder(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "media", "images"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "media/images/a.png"), []byte("image"), 0600); e != nil {
		t.Fatal(e)
	}
	items, e := Scan(Request{Root: root, AssetDir: "media"})
	if e != nil || len(items) != 1 {
		t.Fatalf("custom: %+v %v", items, e)
	}
	if _, e = Scan(Request{Root: root, AssetDir: "../other"}); e == nil {
		t.Fatal("outside attachment directory accepted")
	}
}
