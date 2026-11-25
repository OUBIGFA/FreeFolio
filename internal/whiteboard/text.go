// Wrapping follows Quiet.md board/text_wrapping.rs and Excalidraw 3a317b5
// (MIT). Grapheme segmentation uses the existing go-text dependency.
package whiteboard

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/segmenter"
	"golang.org/x/text/unicode/norm"
)

type Font struct {
	Size   float64
	Family int
}
type MeasureFunc func(string, Font) float64
type TextLine struct {
	Text       string
	Start, End int
}

func jsSpace(c rune) bool {
	return c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r' || c == ' ' || c == '\u00a0' || c == '\u1680' || (c >= 0x2000 && c <= 0x200a) || c == '\u2028' || c == '\u2029' || c == '\u202f' || c == '\u205f' || c == '\u3000' || c == '\ufeff'
}

const cjkExtra = "｀＇＾〃〰〆＃＆＊＋－ー／＼＝｜￤〒￢￣"
const cjkOpen = "（［｛〈《｟｢「『【〖〔〘〚＜〝"
const cjkClose = "）］｝〉》｠｣」』】〗〕〙〛＞。．，、〟‥？！：；・〜〞"
const cjkCurrency = "￥￦￡￠＄"
const commonOpen = "<([{"
const commonClose = ">)]}.,:;!?…/"

func isCJK(c rune) bool {
	return unicode.Is(unicode.Han, c) || unicode.Is(unicode.Hiragana, c) || unicode.Is(unicode.Katakana, c) || unicode.Is(unicode.Hangul, c) || strings.ContainsRune(cjkExtra, c)
}
func isBreak(a, b rune) bool {
	openA := strings.ContainsRune(commonOpen+cjkOpen, a)
	closeB := strings.ContainsRune(commonClose, b)
	return jsSpace(b) || jsSpace(a) || a == '-' || ((isCJK(b) || strings.ContainsRune(cjkCurrency, b)) && !openA) || (isCJK(a) && b != '-' && !closeB && !strings.ContainsRune(cjkClose, b)) || (strings.ContainsRune(cjkOpen, b) && !openA) || (strings.ContainsRune(cjkClose, a) && !strings.ContainsRune(cjkClose, b) && !closeB) || (strings.ContainsRune(commonClose, a) && !closeB && strings.ContainsRune(commonOpen, b))
}
func graphemes(s string) []string {
	var seg segmenter.Segmenter
	seg.Init([]rune(s))
	it := seg.GraphemeIterator()
	var out []string
	for it.Next() {
		g := string(it.Grapheme().Text)
		// Keep the full Excalidraw emoji token when the platform's Unicode
		// table splits a newer pictograph immediately after a ZWJ.
		if len(out) > 0 && strings.HasSuffix(out[len(out)-1], "\u200d") {
			out[len(out)-1] += g
		} else {
			out = append(out, g)
		}
	}
	return out
}
func isEmoji(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return (r >= 0x1f000 && r <= 0x1faff) || (r >= 0x2600 && r <= 0x27ff) || r == 0x2300 || strings.ContainsRune(s, 0xfe0f) || strings.ContainsRune(s, 0x200d) || strings.ContainsRune(s, 0x20e3)
}
func TextTokens(line string) []string {
	s := norm.NFC.String(line)
	var out []string
	p, q := 0, 0
	var prev rune
	for _, g := range graphemes(s) {
		c, _ := utf8.DecodeRuneInString(g)
		if isEmoji(g) {
			if p < q {
				out = append(out, s[p:q])
			}
			out = append(out, g)
			q += len(g)
			p = q
			prev, _ = utf8.DecodeLastRuneInString(g)
			continue
		}
		if q != p && isBreak(prev, c) {
			out = append(out, s[p:q])
			p = q
		}
		prev, _ = utf8.DecodeLastRuneInString(g)
		q += len(g)
	}
	if p < len(s) {
		out = append(out, s[p:])
	}
	return out
}
func ApproxMeasure(s string, f Font) float64 {
	var w float64
	for _, g := range graphemes(s) {
		r, _ := utf8.DecodeRuneInString(g)
		if isCJK(r) || isEmoji(g) {
			w += f.Size
		} else {
			w += f.Size * .55
		}
	}
	return w
}
func WrapText(text string, font Font, maxWidth float64, m MeasureFunc) string {
	lines := WrappedTextLines(text, font, maxWidth, m)
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = l.Text
	}
	return strings.Join(parts, "\n")
}
func WrappedTextLines(text string, f Font, w float64, m MeasureFunc) []TextLine {
	if m == nil {
		m = ApproxMeasure
	}
	var out []TextLine
	offset := 0
	for _, line := range strings.Split(text, "\n") {
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 || m(line, f) <= w {
			out = append(out, TextLine{line, offset, offset + len(line)})
		} else {
			out = append(out, wrapLine(line, offset, f, w, m)...)
		}
		offset += len(line) + 1
	}
	return out
}
func trimSoft(line string, start, end int) TextLine {
	t := strings.TrimRightFunc(line, jsSpace)
	return TextLine{t, start, end - (len(line) - len(t))}
}
func trimLast(line string, start, end int, f Font, w float64, m MeasureFunc) TextLine {
	if m(line, f) <= w {
		return TextLine{line, start, end}
	}
	run := len(line) - len(strings.TrimRightFunc(line, jsSpace))
	_, first := utf8.DecodeRuneInString(line)
	split := max(len(line)-run, first)
	trimmed, spaces := strings.TrimRightFunc(line, jsSpace), ""
	if split < len(line) && !strings.ContainsAny(line[:split], "\n\r\u2028\u2029") {
		trimmed, spaces = line[:split], line[split:]
	}
	width := m(trimmed, f)
	for _, c := range spaces {
		ch := string(c)
		nw := width + m(ch, f)
		if nw > w {
			break
		}
		trimmed += ch
		width = nw
	}
	return TextLine{trimmed, start, end - (len(line) - len(trimmed))}
}
func wrapWord(word string, start int, f Font, w float64, m MeasureFunc) []TextLine {
	if isEmoji(word) {
		return []TextLine{{word, start, start + len(word)}}
	}
	var lines []TextLine
	cur := ""
	curStart, curEnd := start, start
	width := float64(0)
	offset := start
	for _, ch := range graphemes(word) {
		a, b := offset, offset+len(ch)
		cw := m(ch, f)
		offset = b
		if width+cw <= w {
			if cur == "" {
				curStart = a
			}
			cur += ch
			curEnd = b
			width += cw
			continue
		}
		if cur != "" {
			lines = append(lines, TextLine{cur, curStart, curEnd})
		}
		cur, curStart, curEnd, width = ch, a, b, cw
	}
	if cur != "" {
		lines = append(lines, TextLine{cur, curStart, curEnd})
	}
	return lines
}
func wrapLine(line string, start int, f Font, w float64, m MeasureFunc) []TextLine {
	var lines []TextLine
	tokens := TextTokens(line)
	cur := ""
	curStart, curEnd := start, start
	width := float64(0)
	offset := start
	for i := 0; i < len(tokens); {
		token := tokens[i]
		a, b := offset, offset+len(token)
		test := cur + token
		testWidth := m(test, f)
		if utf8.RuneCountInString(token) == 1 {
			testWidth = width + m(token, f)
		}
		if strings.IndexFunc(token, jsSpace) >= 0 || testWidth <= w {
			if cur == "" {
				curStart = a
			}
			cur, curEnd, width, offset = test, b, testWidth, b
			i++
			continue
		}
		if cur == "" {
			wrapped := wrapWord(token, a, f, w, m)
			last := wrapped[len(wrapped)-1]
			lines = append(lines, wrapped[:len(wrapped)-1]...)
			cur, curStart, curEnd = last.Text, last.Start, last.End
			width = m(cur, f)
			offset = b
			i++
		} else {
			lines = append(lines, trimSoft(cur, curStart, curEnd))
			cur = ""
			curStart, curEnd, width = a, a, 0
		}
	}
	if cur != "" {
		lines = append(lines, trimLast(cur, curStart, curEnd, f, w, m))
	}
	return lines
}

func (s *Scene) BindText(textID, containerID string, m MeasureFunc) error {
	t, c := s.ElementByID(textID), s.ElementByID(containerID)
	if t == nil || c == nil || t.Kind != KindText || !(c.Kind == KindRectangle || c.Kind == KindDiamond || c.Kind == KindEllipse || c.Kind == KindNote || c.Kind == KindArrow) || c.BoundTextID() != "" {
		return fmt.Errorf("whiteboard: text cannot be bound to this element")
	}
	s.UnbindText(textID)
	t.ContainerID = c.ID
	t.TextAlign = "center"
	t.VerticalAlign = "middle"
	if c.Kind == KindNote {
		t.TextAlign = "left"
		t.VerticalAlign = "top"
	}
	addBound(c, t.ID, "text")
	s.LayoutText(t.ID, m)
	Touch(t)
	Touch(c)
	return nil
}
func (s *Scene) UnbindText(id string) {
	t := s.ElementByID(id)
	if t == nil {
		return
	}
	if c := s.ElementByID(t.ContainerID); c != nil {
		c.BoundElements = slices.DeleteFunc(c.BoundElements, func(b BoundElement) bool { return b.ID == id && b.Type == "text" })
		Touch(c)
	}
	t.ContainerID = ""
	t.AutoResize = true
	Touch(t)
}
func (s *Scene) SetText(id, text string, m MeasureFunc) {
	e := s.ElementByID(id)
	if e == nil || e.Kind != KindText {
		return
	}
	e.OriginalText = text
	e.Text = text
	s.LayoutText(id, m)
	Touch(e)
}
func (s *Scene) LayoutText(id string, m MeasureFunc) {
	e := s.ElementByID(id)
	if e == nil || e.Kind != KindText {
		return
	}
	if m == nil {
		m = ApproxMeasure
	}
	f := Font{e.FontSize, e.FontFamily}
	if f.Size <= 0 {
		f.Size = 20
		e.FontSize = 20
	}
	lh := e.LineHeight
	if lh <= 0 {
		lh = FontLineHeight(f.Family)
		e.LineHeight = lh
	}
	text := e.OriginalText
	if text == "" {
		text = e.Text
	}
	width := math.Inf(1)
	c := s.ElementByID(e.ContainerID)
	padding := float64(5)
	var bounds Box
	if c != nil {
		bounds = Box{c.X, c.Y, c.W, c.H}
		if c.Kind == KindNote {
			padding = 16
		}
		if c.Kind == KindDiamond {
			bounds = Box{c.X + c.W/4, c.Y + c.H/4, c.W / 2, c.H / 2}
		} else if c.Kind == KindEllipse {
			bounds = Box{c.X + c.W*.15, c.Y + c.H*.15, c.W * .7, c.H * .7}
		}
		if c.Shape != "" {
			bounds = dimContentBounds(c)
		}
		width = math.Max(1, bounds.W-padding*2)
	} else if !e.AutoResize {
		width = math.Max(1, e.W)
	}
	e.Text = WrapText(text, f, width, m)
	lines := strings.Split(e.Text, "\n")
	w := float64(0)
	for _, line := range lines {
		w = math.Max(w, m(line, f))
	}
	if e.AutoResize || c != nil {
		e.W = w
	}
	e.H = float64(len(lines)) * f.Size * lh
	if c != nil {
		if e.H+padding*2 > bounds.H {
			d := e.H + padding*2 - bounds.H
			c.H += d
			bounds.H += d
			Touch(c)
		}
		e.X = bounds.X + padding
		e.Y = bounds.Y + padding
		if e.TextAlign == "center" {
			e.X = bounds.X + (bounds.W-e.W)/2
		} else if e.TextAlign == "right" {
			e.X = bounds.X + bounds.W - padding - e.W
		}
		if e.VerticalAlign == "middle" {
			e.Y = bounds.Y + (bounds.H-e.H)/2
		} else if e.VerticalAlign == "bottom" {
			e.Y = bounds.Y + bounds.H - padding - e.H
		}
		e.Angle = c.Angle
		if c.Kind == KindArrow && len(c.Points) > 1 {
			mid := Lerp(c.Points[0], c.Points[len(c.Points)-1], .5)
			e.X = c.X + mid[0] - e.W/2
			e.Y = c.Y + mid[1] - e.H/2
			e.Angle = 0
		}
	}
}
