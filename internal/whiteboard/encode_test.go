package whiteboard

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestEditableImagesCarryUnicodeScene(t *testing.T) {
	s := NewScene()
	e := NewElement(KindText, 10, 20, 100, 30)
	e.Text, e.OriginalText = "架构 🐈", "架构 🐈"
	s.Elements = append(s.Elements, e)
	text, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := EncodeSVGPayload(text)
	if err != nil {
		t.Fatal(err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><metadata>` + metadata + `</metadata></svg>`
	loaded, err := Decode(svg)
	if err != nil || loaded.Elements[0].Text != "架构 🐈" {
		t.Fatalf("SVG scene: %+v, %v", loaded, err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	embedded, err := EncodePNGMetadata(buf.Bytes(), text)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(embedded)); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePNGMetadata(embedded)
	if err != nil || decoded != text {
		t.Fatalf("PNG scene lost: %v", err)
	}
	// ArcMD's older SVG uses uncompressed UTF-8 JSON under the same markers.
	legacy := "<svg><metadata><!-- payload-type:application/vnd.excalidraw+json --><!-- payload-version:2 --><!-- payload-start -->" + base64.StdEncoding.EncodeToString([]byte(text)) + "<!-- payload-end --></metadata></svg>"
	if got, err := Decode(legacy); err != nil || got.Elements[0].Text != e.Text {
		t.Fatalf("legacy SVG: %v", err)
	}
	if _, err := DecodePNGMetadata(embedded[:len(embedded)-5]); err == nil {
		t.Fatal("truncated PNG accepted")
	}
	if _, err := DecodeSVGPayload(strings.Replace(svg, "payload-version:2", "payload-version:99", 1)); err == nil {
		t.Fatal("future payload version accepted")
	}
}
