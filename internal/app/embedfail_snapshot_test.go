package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/FreeMD/internal/editor"
	"github.com/OUBIGFA/FreeMD/internal/vault"
)

func TestEmbedFailureSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEMD_SNAPSHOT to render a failed embed")
	a, tt := newTestApp(t)
	url := "https://x.com/i/status/1834693009369403543"
	a.remote.entries[url] = &remoteImage{resource: editor.ImageResource{Problem: editor.ImageEmbed}}
	if err := vault.Write(root(a), "Embed.md", "# 嵌入\n\n前一段。\n\n![]("+url+")\n\n后一段。\n"); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("Embed.md")
	tt.SetSize(1120, 640)
	for range 3 {
		tt.Frame()
		tt.Advance(200 * time.Millisecond)
	}
	savePNG(t, filepath.Join(dir, "embed-failed.png"), tt)
	if err := tt.Click("嵌入的内容不存在或无法访问：" + url); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tt.Frame()
		tt.Advance(200 * time.Millisecond)
	}
	savePNG(t, filepath.Join(dir, "embed-editing.png"), tt)
}
