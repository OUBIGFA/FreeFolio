package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Ported from ArcMD 12d2e01 and 14e92c1: inline menus must not hold the
// arrow keys after a paste, with no matches, or under a modifier.
func TestSlashMenuPasteAndEmptyResultsDoNotBlockArrows(t *testing.T) {
	t.Run("paste-after-slash-dismisses-menu", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.Type("/")
		if !e.slashOpen {
			t.Fatal("expected slash menu after typing /")
		}
		tt.SetClipboard("hello world")
		tt.Key(ui.Cmd, ui.KeyV)
		tt.Frame()
		if e.slashOpen {
			t.Fatal("expected paste after / to dismiss slash menu")
		}
		if e.Text() != "/hello world" {
			t.Fatalf("unexpected text after paste: %q", e.Text())
		}
		end := e.Caret
		tt.Key(0, ui.KeyLeft)
		if e.Caret != end-1 {
			t.Fatalf("KeyLeft did not move caret: got %d want %d", e.Caret, end-1)
		}
		tt.Key(0, ui.KeyRight)
		if e.Caret != end {
			t.Fatalf("KeyRight did not move caret: got %d want %d", e.Caret, end)
		}
		tt.Key(0, ui.KeyEnter)
		if len(e.Doc.Blocks) != 2 {
			t.Fatalf("KeyEnter did not insert block: %d", len(e.Doc.Blocks))
		}
	})

	t.Run("paste-text-starting-with-slash-keeps-menu-closed", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.SetClipboard("/usr/local/bin")
		tt.Key(ui.Cmd, ui.KeyV)
		tt.Frame()
		if e.slashOpen {
			t.Fatal("pasting a path that starts with / opened the slash menu")
		}
		end := e.Caret
		tt.Key(0, ui.KeyLeft)
		if e.Caret != end-1 {
			t.Fatalf("KeyLeft swallowed after pasting /...: got %d want %d", e.Caret, end-1)
		}
	})

	t.Run("deleting-slash-lets-menu-reopen", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.Type("/")
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if e.slashOpen {
			t.Fatal("Escape did not close the slash menu")
		}
		tt.Key(0, ui.KeyBackspace)
		tt.Frame()
		tt.Type("/")
		tt.Frame()
		if !e.slashOpen {
			t.Fatal("slash menu did not reopen after deleting and retyping /")
		}
	})

	t.Run("empty-slash-results-allow-arrows-and-enter", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.Type("/zzz")
		if !e.slashOpen || len(e.slashEntries()) != 0 || !tt.HasText("未能找到相关结果") {
			t.Fatalf("expected empty slash menu, open=%v len=%d", e.slashOpen, len(e.slashEntries()))
		}
		end := e.Caret
		tt.Key(0, ui.KeyLeft)
		if e.Caret != end-1 {
			t.Fatalf("KeyLeft swallowed on empty slash results: got %d want %d", e.Caret, end-1)
		}
		tt.Key(0, ui.KeyRight)
		if e.Caret != end {
			t.Fatalf("KeyRight swallowed on empty slash results: got %d want %d", e.Caret, end)
		}
		tt.Key(0, ui.KeyEnter)
		if len(e.Doc.Blocks) != 2 || e.slashOpen {
			t.Fatalf("KeyEnter swallowed on empty slash results: blocks=%d open=%v", len(e.Doc.Blocks), e.slashOpen)
		}
	})

	t.Run("modified-arrows-pass-through-open-menu", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.Type("/")
		if !e.slashOpen {
			t.Fatal("expected slash menu after typing /")
		}
		idx := e.slashIndex
		tt.Key(ui.Shift, ui.KeyLeft)
		if e.slashIndex != idx {
			t.Fatalf("Shift+Left moved the menu selection: %d -> %d", idx, e.slashIndex)
		}
		if e.Anchor == e.Caret {
			t.Fatal("Shift+Left did not extend the text selection")
		}
	})

	t.Run("empty-slash-query-keeps-grid-horizontal-arrows", func(t *testing.T) {
		e, tt := newTester("")
		focus(e, tt)
		tt.Type("/")
		if !e.slashOpen || e.slashIndex != 0 || !e.slashEntries()[0].grid {
			t.Fatalf("expected grid slash menu at index 0, open=%v idx=%d", e.slashOpen, e.slashIndex)
		}
		tt.Key(0, ui.KeyRight)
		if e.slashIndex != 1 {
			t.Fatalf("expected KeyRight in icon grid to move slashIndex to 1, got %d", e.slashIndex)
		}
	})
}
