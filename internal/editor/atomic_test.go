package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

// Ported from ArcMD 14e92c1: a picture that failed to load is picked by a
// click and offers replace, reveal and delete from its context menu.
func TestBrokenImageClickAndContextMenu(t *testing.T) {
	var chose, revealed string
	e, tt := imageTester("Before\n\n![description](photo.png)\n\nAfter", 600, ImageResource{Problem: ImageMissing, Local: true},
		func(id, src string) { chose = src }, func(src string) { revealed = src })
	label := ""
	for _, a := range tt.AccessAll() {
		if strings.HasPrefix(a.Name, "无法显示图片：") {
			label = a.Name
		}
	}
	if label == "" {
		t.Fatal("missing broken picture placeholder")
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := e.Engine.LeafAt(e.Caret).Block; got.Kind != "image" {
		t.Fatalf("placeholder click left the caret on %q", got.Kind)
	}
	if err := tt.RightClick(label); err != nil {
		t.Fatal(err)
	}
	menu := strings.Join(tt.Menu(), "|")
	for _, want := range []string{"重新选择图片…", "在文件夹中显示", "删除"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("placeholder menu %q lacks %q", menu, want)
		}
	}
	if err := tt.ChooseMenuItem("重新选择图片…"); err != nil {
		t.Fatal(err)
	}
	if chose != "photo.png" {
		t.Fatalf("replace got %q", chose)
	}
	tt.Frame()
	if err := tt.RightClick(label); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("在文件夹中显示"); err != nil {
		t.Fatal(err)
	}
	if revealed != "photo.png" {
		t.Fatalf("reveal got %q", revealed)
	}
	tt.Frame()
	if err := tt.RightClick(label); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "Before" || e.Doc.Blocks[1].Text() != "After" {
		t.Fatalf("placeholder delete left %d blocks", len(e.Doc.Blocks))
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if len(e.Doc.Blocks) != 3 || e.Doc.Blocks[1].URL != "photo.png" {
		t.Fatal("placeholder delete could not be undone")
	}
}

// An inline whiteboard is one object: Backspace and Delete on it remove it
// whole, copy and cut take the whole block, and undo restores its scene.
func TestInlineWhiteboardDeleteCopyCutUndo(t *testing.T) {
	raw, err := whiteboard.NewScene().Encode()
	if err != nil {
		t.Fatal(err)
	}
	md := "Before\n\n```excalidraw\n" + raw + "\n```\n\nAfter"
	for _, key := range []ui.Key{ui.KeyBackspace, ui.KeyDelete} {
		e, tt := newTester(md)
		focus(e, tt)
		if !isWhiteboardBlock(e.Doc.Blocks[1]) {
			t.Fatalf("block 1 is %q", e.Doc.Blocks[1].Kind)
		}
		leaf := e.Doc.Leaves()[1]
		e.SetSelection(leaf.Start+2, leaf.Start+2)
		tt.Frame()
		tt.Key(0, key)
		tt.Frame()
		if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "Before" || e.Doc.Blocks[1].Text() != "After" {
			t.Fatalf("key %v did not remove the whiteboard whole: %d blocks", key, len(e.Doc.Blocks))
		}
		if key == ui.KeyBackspace && e.Caret != e.Doc.Leaves()[0].End {
			t.Fatalf("Backspace left the caret at %d, want end of Before", e.Caret)
		}
		tt.Key(ui.Cmd, ui.KeyZ)
		tt.Frame()
		if len(e.Doc.Blocks) != 3 || !isWhiteboardBlock(e.Doc.Blocks[1]) || e.Doc.Blocks[1].Text() != raw {
			t.Fatal("undo did not restore the whiteboard scene")
		}
	}

	e, tt := newTester(md)
	focus(e, tt)
	leaf := e.Doc.Leaves()[1]
	e.SetSelection(leaf.Start+2, leaf.Start+2)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	if clip := tt.Clipboard(); !strings.Contains(clip, "excalidraw") {
		t.Fatalf("copy did not take the whiteboard block: %q", clip)
	}
	tt.Key(ui.Cmd, ui.KeyX)
	tt.Frame()
	if len(e.Doc.Blocks) != 2 {
		t.Fatalf("cut left %d blocks", len(e.Doc.Blocks))
	}

	// A range from Before into the whiteboard takes it whole, leaking no JSON.
	e, tt = newTester(md)
	focus(e, tt)
	leaves := e.Doc.Leaves()
	e.SetSelection(leaves[0].Start+3, leaves[1].Start+5)
	tt.Frame()
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "paragraph" || e.Doc.Blocks[0].Text() != "Bef" || e.Doc.Blocks[0].Language != "" {
		t.Fatalf("range delete into the whiteboard leaked: %q %q", e.Doc.Blocks[0].Kind, e.Doc.Blocks[0].Text())
	}
}
