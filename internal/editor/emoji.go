package editor

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// emojiFamily is the system's color emoji font.
func emojiFamily() string {
	switch runtime.GOOS {
	case "windows":
		return "Segoe UI Emoji"
	case "darwin":
		return "Apple Color Emoji"
	}
	return "Noto Color Emoji"
}

// colorKeycaps gives keycap emoji, as 1️⃣ (a digit, # or * with an optional
// U+FE0F and the enclosing keycap U+20E3), the color emoji font. The text
// fonts hold the digit, so font fallback draws it in them with the keycap
// as a box or an outline. Only fonts change: the text and its offsets stay.
func colorKeycaps(spans []ui.Span) []ui.Span {
	has := false
	for _, sp := range spans {
		if strings.ContainsRune(sp.Text, '\u20e3') {
			has = true
			break
		}
	}
	if !has {
		return spans
	}
	family := emojiFamily()
	out := make([]ui.Span, 0, len(spans)+2)
	for _, sp := range spans {
		if sp.ObjectWidth != 0 || !strings.ContainsRune(sp.Text, '\u20e3') {
			out = append(out, sp)
			continue
		}
		text := sp.Text
		start := 0
		for i := 0; i < len(text); i++ {
			c := text[i]
			if !(c >= '0' && c <= '9' || c == '#' || c == '*') {
				continue
			}
			end := i + 1
			if strings.HasPrefix(text[end:], "\ufe0f") {
				end += len("\ufe0f")
			}
			if !strings.HasPrefix(text[end:], "\u20e3") {
				continue
			}
			end += len("\u20e3")
			if i > start {
				before := sp
				before.Text = text[start:i]
				out = append(out, before)
			}
			key := sp
			key.Text, key.Font = text[i:end], family
			out = append(out, key)
			start = end
			i = end - 1
		}
		if start < len(text) {
			rest := sp
			rest.Text = text[start:]
			out = append(out, rest)
		}
	}
	return out
}
