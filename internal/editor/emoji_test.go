package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Keycap emoji get the color emoji font; the text and its order stay.
func TestColorKeycapsSplitsOnlyKeycaps(t *testing.T) {
	in := []ui.Span{{Text: "1️⃣ 标题 #⃣x", Weight: 500}, {Text: "plain 2"}}
	out := colorKeycaps(in)
	var joined strings.Builder
	keys := 0
	for _, sp := range out {
		joined.WriteString(sp.Text)
		if sp.Font == emojiFamily() {
			keys++
			if sp.Weight != 500 {
				t.Fatalf("keycap lost its style: %+v", sp)
			}
		}
	}
	if joined.String() != "1️⃣ 标题 #⃣xplain 2" || keys != 2 {
		t.Fatalf("got %q with %d keycaps: %+v", joined.String(), keys, out)
	}
}
