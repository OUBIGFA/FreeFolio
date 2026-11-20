package whiteboard

import (
	"testing"
	"unicode/utf16"
)

// Cases retain Excalidraw/Quiet.md textWrapping expected values. The upstream
// measurement stub is ten pixels per UTF-16 unit, independent of wrapping.
func TestReferenceTextWrapping(t *testing.T) {
	measure := func(s string, font Font) float64 { return float64(len(utf16.Encode([]rune(s))) * 10) }
	for _, tc := range []struct {
		text  string
		width float64
		want  string
	}{
		{"Hello Excalidraw", 100, "Hello\nExcalidraw"},
		{"Hello😀", 10, "H\ne\nl\nl\no\n😀"},
		{"Hello     ", 60, "Hello "},
		{"  Hello  World", 90, "  Hello\nWorld"},
		{"   Hello  World            ", 90, "   Hello\nWorld    "},
		{"Hello   Wo rl  d                     ", 100, "Hello   Wo\nrl  d     "},
		{"😀🗺🔥👩🏽‍🦰👨‍👩‍👧‍👦🇨🇿", 1, "😀\n🗺\n🔥\n👩🏽‍🦰\n👨‍👩‍👧‍👦\n🇨🇿"},
		{"안녕하세요こんにちは世界ｺﾝﾆﾁハ你好", 30, "안녕하\n세요こ\nんにち\nは世界\nｺﾝﾆ\nﾁハ你\n好"},
		{"Hello「たWorld」", 60, "Hello\n「た\nWorld」"},
		{"「Helloた」World", 70, "「Hello\nた」World"},
		{"HelloたWorld", 50, "Hello\nた\nWorld"},
		{"Hello thereusing-now", 100, "Hello\nthereusing\n-now"},
	} {
		if got := WrapText(tc.text, Font{Size: 10, Family: 3}, tc.width, measure); got != tc.want {
			t.Errorf("wrap %q at %g: %q want %q", tc.text, tc.width, got, tc.want)
		}
	}
}

func TestBoundTextLayoutAndUnbind(t *testing.T) {
	m := func(s string, font Font) float64 { return float64(len([]rune(s))) * font.Size / 2 }
	s := NewScene()
	box := NewElement(KindRectangle, 20, 30, 150, 80)
	label := NewElement(KindText, 0, 0, 0, 0)
	label.Text = "linked label"
	label.OriginalText = label.Text
	s.Elements = []*Element{box, label}
	if err := s.BindText(label.ID, box.ID, m); err != nil {
		t.Fatal(err)
	}
	if label.ContainerID != box.ID || box.BoundTextID() != label.ID || label.X < box.X || label.Y < box.Y {
		t.Fatal("text not bound inside shape")
	}
	s.SetText(label.ID, "a longer sentence that wraps", m)
	if label.OriginalText != "a longer sentence that wraps" || label.H == 0 {
		t.Fatal("original text lost")
	}
	s.UnbindText(label.ID)
	if label.ContainerID != "" || box.BoundTextID() != "" {
		t.Fatal("unbind left dangling relationship")
	}
}
