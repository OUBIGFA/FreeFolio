package editor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/OUBIGFA/FreeMD/internal/whiteboard"
	"github.com/egoist/mygo/transfer"
	"golang.org/x/net/html"
)

const boardClipFormat transfer.Format = "application/vnd.excalidraw+json"
const boardSVGFormat transfer.Format = "image/svg+xml"

func (b *Board) copyBoard(all bool) {
	ids := b.SelectedIDs()
	if all || len(ids) == 0 {
		for _, e := range b.Scene.Elements {
			if !e.IsDeleted {
				ids = append(ids, e.ID)
			}
		}
	}
	text, err := b.Scene.Clipboard(ids)
	if err != nil {
		b.err = err
		return
	}
	b.services.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(boardClipFormat, []byte(text)), transfer.Bytes(transfer.Text, []byte(text)))))
}
func (b *Board) copySVG() {
	s := b.selectedScene()
	o := b.options
	work := func() (string, error) {
		return BoardSVG(s, o)
	}
	done := func(svg string, err error) {
		if err != nil {
			b.err = err
			return
		}
		b.services.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(boardSVGFormat, []byte(svg)), transfer.Bytes(transfer.Text, []byte(svg)))))
	}
	if o.Dispatch != nil {
		go func() { svg, err := work(); o.Dispatch(func() { done(svg, err) }) }()
	} else {
		done(work())
	}
}
func (b *Board) copyPNG() {
	s, o := b.selectedScene(), b.options
	done := func(data []byte, err error) {
		if err != nil {
			b.err = err
			return
		}
		b.services.WriteClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, data))))
	}
	if o.Dispatch != nil {
		go func() { data, err := BoardPNG(s, o); o.Dispatch(func() { done(data, err) }) }()
	} else {
		done(BoardPNG(s, o))
	}
}
func (b *Board) pasteScene(s *whiteboard.Scene, o BoardOptions) bool {
	if s == nil {
		return false
	}
	var ids []string
	for _, e := range s.Elements {
		if !e.IsDeleted {
			ids = append(ids, e.ID)
		}
	}
	clip, err := s.Clipboard(ids)
	if err != nil {
		b.err = err
		return false
	}
	b.edit(o, func() {
		at := b.hover
		if at == (whiteboard.Point{}) {
			at = b.scenePoint(b.lastRect.W/2, b.lastRect.H/2)
		}
		newIDs, err := b.Scene.Paste(clip, at)
		if err != nil {
			b.err = err
			return
		}
		b.selectIDs(newIDs)
	})
	return b.err == nil
}
func (b *Board) pasteBoard(o BoardOptions) {
	data := b.services.ReadClipboardData(boardClipFormat, boardSVGFormat, transfer.PNG, transfer.FileList, transfer.HTML, transfer.Text)
	if files, err := data.Files(); err == nil && len(files) > 0 && o.DropFiles != nil {
		o.DropFiles(files)
		return
	}
	if raw, err := data.Read(transfer.PNG); err == nil && len(raw) > 0 {
		scene, err := whiteboard.DecodePNGMetadata(raw)
		if err == nil {
			s, err := whiteboard.Decode(scene)
			if err != nil {
				b.err = err
				return
			}
			b.pasteScene(s, o)
			return
		}
		if !errors.Is(err, whiteboard.ErrNoScene) {
			b.err = err
			return
		}
		b.err = b.InsertImage(raw, b.hover)
		return
	}
	for _, format := range []transfer.Format{boardClipFormat, boardSVGFormat, transfer.Text} {
		raw, err := data.Read(format)
		if err != nil || len(raw) == 0 {
			continue
		}
		text := string(raw)
		var marker struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &marker)
		if marker.Type == "excalidraw/clipboard" {
			b.edit(o, func() {
				ids, err := b.Scene.Paste(text, b.hover)
				if err != nil {
					b.err = err
					return
				}
				b.selectIDs(ids)
			})
			return
		}
		if marker.Type == "excalidraw" || strings.Contains(text, "payload-type:"+whiteboard.SceneMIME) {
			s, err := whiteboard.Decode(text)
			if err != nil {
				b.err = err
				return
			}
			b.pasteScene(s, o)
			return
		}
	}
	text := b.services.ReadClipboard()
	if raw, err := data.Read(transfer.HTML); err == nil && len(raw) > 0 {
		node, err := html.Parse(bytes.NewReader(raw))
		if err == nil {
			var out strings.Builder
			var walk func(*html.Node)
			walk = func(n *html.Node) {
				if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
					return
				}
				if n.Type == html.TextNode {
					out.WriteString(n.Data)
				}
				if n.Type == html.ElementNode && n.Data == "br" {
					out.WriteByte('\n')
				}
				for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
					walk(ch)
				}
				if n.Type == html.ElementNode && (n.Data == "p" || n.Data == "div") {
					out.WriteByte('\n')
				}
			}
			walk(node)
			if strings.TrimSpace(out.String()) != "" {
				text = strings.TrimSpace(out.String())
			}
		}
	}
	if len(text) > 1<<20 {
		b.err = fmt.Errorf("clipboard text exceeds limit")
		return
	}
	if text != "" {
		b.edit(o, func() {
			e := b.newElement(whiteboard.KindText, b.hover, 0, 0)
			e.OriginalText = text
			e.Text = text
			b.Scene.Elements = append(b.Scene.Elements, e)
			b.Scene.LayoutText(e.ID, boardMeasure)
			b.selectIDs([]string{e.ID})
		})
	}
}
