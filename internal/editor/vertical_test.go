package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Ported from ArcMD 14e92c1.
func TestVerticalArrowNavigation(t *testing.T) {
	t.Run("headings-and-paragraphs", func(t *testing.T) {
		md := "# asdas\n\n## dasd\n\nasdas\n\n## asdad\n\nasdasd\n\nasda"
		e, tt := newTester(md)
		focus(e, tt)

		// Put caret in the middle of the second H2 ("asdad", block 3).
		h2Leaf, ok := e.Engine.LeafByID(e.Doc.Blocks[3].ID)
		if !ok {
			t.Fatal("missing second H2 block")
		}
		e.SetSelection(h2Leaf.Start+3, h2Leaf.Start+3)
		tt.Frame()

		// Walk up block-by-block from block 3 -> 2 -> 1 -> 0.
		for want := 2; want >= 0; want-- {
			tt.Key(0, ui.KeyUp)
			got := e.Engine.LeafAt(e.Caret).Block.ID
			if got != e.Doc.Blocks[want].ID {
				t.Fatalf("KeyUp from block %d: got block %q want %q (caret=%d)", want+1, got, e.Doc.Blocks[want].ID, e.Caret)
			}
		}

		// Pressing KeyUp on the first block stays in block 0.
		before := e.Caret
		tt.Key(0, ui.KeyUp)
		if e.Caret != before {
			t.Fatalf("KeyUp on first block moved caret from %d to %d", before, e.Caret)
		}

		// Walk all the way down from block 0 -> 5.
		for want := 1; want < len(e.Doc.Blocks); want++ {
			tt.Key(0, ui.KeyDown)
			got := e.Engine.LeafAt(e.Caret).Block.ID
			if got != e.Doc.Blocks[want].ID {
				t.Fatalf("KeyDown to block %d: got block %q want %q (caret=%d)", want, got, e.Doc.Blocks[want].ID, e.Caret)
			}
		}
	})

	t.Run("code-block-and-callout", func(t *testing.T) {
		md := "before\n\n```go\nline1\nline2\n```\n\n> [!NOTE]\n> callout text\n\nafter"
		e, tt := newTester(md)
		focus(e, tt)

		// Start in "before" (block 0), step down into code line 1 -> code line 2 -> callout -> after.
		b0, _ := e.Engine.LeafByID(e.Doc.Blocks[0].ID)
		e.SetSelection(b0.Start+2, b0.Start+2)
		tt.Frame()

		tt.Key(0, ui.KeyDown)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[1].ID {
			t.Fatalf("KeyDown into code block failed: got %q want %q", got, e.Doc.Blocks[1].ID)
		}
		codeLine1 := e.Caret

		tt.Key(0, ui.KeyDown)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[1].ID || e.Caret <= codeLine1 {
			t.Fatalf("KeyDown within code block failed: caret=%d prev=%d block=%q", e.Caret, codeLine1, got)
		}

		tt.Key(0, ui.KeyDown)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[2].ID {
			t.Fatalf("KeyDown out of code block into callout failed: got %q want %q", got, e.Doc.Blocks[2].ID)
		}

		tt.Key(0, ui.KeyDown)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[3].ID {
			t.Fatalf("KeyDown out of callout failed: got %q want %q", got, e.Doc.Blocks[3].ID)
		}

		// Now walk back up: after -> callout -> code line 2 -> code line 1 -> before.
		tt.Key(0, ui.KeyUp)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[2].ID {
			t.Fatalf("KeyUp into callout failed: got %q want %q", got, e.Doc.Blocks[2].ID)
		}
		tt.Key(0, ui.KeyUp)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[1].ID {
			t.Fatalf("KeyUp into code block failed: got %q want %q", got, e.Doc.Blocks[1].ID)
		}
		codeLine2 := e.Caret
		tt.Key(0, ui.KeyUp)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[1].ID || e.Caret >= codeLine2 {
			t.Fatalf("KeyUp within code block failed: caret=%d prev=%d block=%q", e.Caret, codeLine2, got)
		}
		tt.Key(0, ui.KeyUp)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[0].ID {
			t.Fatalf("KeyUp out of code block failed: got %q want %q", got, e.Doc.Blocks[0].ID)
		}
	})

	t.Run("table-columns", func(t *testing.T) {
		md := "above\n\n| A1 | B1 |\n| --- | --- |\n| A2 | B2 |\n\nbelow"
		e, tt := newTester(md)
		focus(e, tt)

		table := e.Doc.Blocks[1]
		b2 := table.Children[1].Children[1]
		b1 := table.Children[0].Children[1]
		b2Leaf, _ := e.Engine.LeafByID(b2.ID)
		e.SetSelection(b2Leaf.Start+1, b2Leaf.Start+1)
		tt.Frame()

		// B2 -> B1 -> above.
		tt.Key(0, ui.KeyUp)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != b1.ID {
			t.Fatalf("KeyUp from B2: got %q want B1 (%q)", got, b1.ID)
		}
		tt.Key(0, ui.KeyUp)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[0].ID {
			t.Fatalf("KeyUp from B1: got %q want above (%q)", got, e.Doc.Blocks[0].ID)
		}

		// Back down from B1 -> B2 -> below.
		b1Leaf, _ := e.Engine.LeafByID(b1.ID)
		e.SetSelection(b1Leaf.Start+1, b1Leaf.Start+1)
		tt.Frame()
		tt.Key(0, ui.KeyDown)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != b2.ID {
			t.Fatalf("KeyDown from B1: got %q want B2 (%q)", got, b2.ID)
		}
		tt.Key(0, ui.KeyDown)
		if got := e.Engine.LeafAt(e.Caret).Block.ID; got != e.Doc.Blocks[2].ID {
			t.Fatalf("KeyDown from B2: got %q want below (%q)", got, e.Doc.Blocks[2].ID)
		}
	})
}
