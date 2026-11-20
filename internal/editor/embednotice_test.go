package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// An embed that cannot show is a notice that stays editable: a click opens
// its address, Enter replaces it, and the Markdown keeps the new link.
func TestEmbedNoticeEditsAddress(t *testing.T) {
	gone := "https://x.com/i/status/1834693009369403543"
	e := New("Before\n\n![](" + gone + ")\n\nAfter")
	load := func(src string) ImageResource { return ImageResource{Problem: ImageEmbed} }
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), LoadImage: load})
	}, 900, 650)
	tt.Frame()
	tt.Frame()
	label := ImageEmbed.text() + "：" + gone
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if e.embedEdit.draft != gone {
		t.Fatalf("address not opened: %+v", e.embedEdit)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://x.com/i/status/1")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if md := ExportMarkdown(e.Doc); !strings.Contains(md, "![](https://x.com/i/status/1)") || e.embedEdit.id != "" {
		t.Fatalf("address not replaced: %q %+v", md, e.embedEdit)
	}
}
