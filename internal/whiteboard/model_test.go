package whiteboard

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The external fixture catches lost images, labels, bindings and extension fields.
func TestExternalSceneSurvivesEditAndReload(t *testing.T) {
	data, err := os.ReadFile("testdata/all-elements.excalidraw")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Elements) != 11 || len(s.Files) != 1 {
		t.Fatalf("scene has %d elements and %d files", len(s.Elements), len(s.Files))
	}
	img := s.ElementByID("img1")
	if img.Kind != KindImage || img.FileID != "file1" || img.Crop == nil || img.Crop.NaturalWidth != 640 || img.Scale[1] != -1 {
		t.Fatalf("image lost: %+v", img)
	}
	label := s.ElementByID("text1")
	if label.ContainerID != "rect1" || label.FontFamily != 5 || label.OriginalText != "Hello world" {
		t.Fatalf("label lost: %+v", label)
	}
	a := s.ElementByID("arrow1")
	if a.StartBinding == nil || a.StartBinding.FixedPoint != (Point{1, .25}) || a.EndBinding.Mode != "inside" {
		t.Fatalf("binding lost: %+v", a)
	}
	s.ElementByID("rect1").X = 100.1234567890123
	encoded, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if again.ElementByID("rect1").X != 100.1234567890123 || again.Files["file1"].DataURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatal("precision or image content changed")
	}
	var fields map[string]any
	if err := json.Unmarshal(again.ElementByID("rect1").Extra["customFlag"], &fields); err != nil || !reflect.DeepEqual(fields["nested"], []any{float64(1), float64(2.5), "x"}) {
		t.Fatalf("extension lost: %s, %v", again.ElementByID("rect1").Extra["customFlag"], err)
	}
	if !strings.Contains(again.PlainText(), "Hello world") || !strings.Contains(again.PlainText(), "Group A") {
		t.Fatal("semantic text missing")
	}
	clone := again.Clone()
	clone.ElementByID("arrow1").Points[0][0] = 99
	clone.Files["file1"].DataURL = "changed"
	if again.ElementByID("arrow1").Points[0][0] != 0 || again.Files["file1"].DataURL == "changed" {
		t.Fatal("clone aliases mutable scene")
	}
}

func TestInvalidSceneCannotBecomeEmptyDocument(t *testing.T) {
	for _, input := range []string{
		``, `null`, `[]`, `{}`, `{"type":"excalidraw","version":99,"elements":[]}`,
		`{"type":"excalidraw","version":2,"elements":[{"id":"x","type":"future-shape"}]}`,
		`{"type":"excalidraw","version":2,"elements":[{"id":"x","type":"rectangle"},{"id":"x","type":"ellipse"}]}`,
		`{"type":"excalidraw","version":2,"elements":[{"id":"x","type":"image","fileId":"f"}],"files":{"f":{"id":"f","path":"../../secret"}}}`,
		`{"type":"excalidraw","version":2,"elements":[]} trailing`,
		`{"type":"excalidraw","version":2}`,
		`{"type":"excalidraw","version":2,"elements":[{"id":"x","type":"rectangle","width":1e99}]}`,
	} {
		if _, err := Decode(input); err == nil {
			t.Fatalf("unsafe input accepted: %q", input)
		}
	}
}

func TestSceneUnknownFieldsAndZeroOpacityRemain(t *testing.T) {
	s, err := Decode(`{"type":"excalidraw","version":2,"vendor":{"a":7},"appState":{"viewBackgroundColor":"#fff","future":true},"elements":[{"id":"x","type":"rectangle","opacity":0,"seed":42,"customData":{"nested":{"k":1}},"future":[1,2]}],"files":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	el := raw["elements"].([]any)[0].(map[string]any)
	if el["opacity"] != float64(0) || raw["vendor"].(map[string]any)["a"] != float64(7) || raw["appState"].(map[string]any)["future"] != true || len(el["future"].([]any)) != 2 {
		t.Fatalf("fields changed: %s", out)
	}
}

func TestClipboardRemapsRelationshipsAndCarriesImages(t *testing.T) {
	data, _ := os.ReadFile("testdata/all-elements.excalidraw")
	s, err := Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	clip, err := s.Clipboard([]string{"rect1", "text1", "arrow1", "ell1", "img1", "frame1"})
	if err != nil {
		t.Fatal(err)
	}
	dst := NewScene()
	ids, err := dst.Paste(clip, Point{200, 300})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 6 || len(dst.Files) != 1 {
		t.Fatalf("paste lost content: %d, %d", len(ids), len(dst.Files))
	}
	for _, el := range dst.Elements {
		if s.ElementByID(el.ID) != nil {
			t.Fatal("paste reused source ID")
		}
		if el.ContainerID != "" && dst.ElementByID(el.ContainerID) == nil {
			t.Fatal("orphan label")
		}
		if el.StartBinding != nil && dst.ElementByID(el.StartBinding.ElementID) == nil {
			t.Fatal("orphan arrow")
		}
		if el.FrameID != "" && dst.ElementByID(el.FrameID) == nil {
			t.Fatal("orphan frame")
		}
	}
}

func TestExternalFalseAndNullFieldsSurvive(t *testing.T) {
	s, err := Decode(`{"type":"excalidraw","version":2,"elements":[{"id":"t","type":"text","text":"wrapped","originalText":"wrapped","autoResize":false},{"id":"a","type":"arrow","points":[[0,0],[100,0]],"startArrowhead":null,"endArrowhead":null}],"files":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Elements []map[string]any }
	if err = json.Unmarshal([]byte(encoded), &out); err != nil {
		t.Fatal(err)
	}
	if value, ok := out.Elements[0]["autoResize"]; !ok || value != false {
		t.Fatal("false text autoresize disappeared")
	}
	for _, name := range []string{"startArrowhead", "endArrowhead"} {
		if value, ok := out.Elements[1][name]; !ok || value != nil {
			t.Fatal("null arrowhead disappeared")
		}
	}
}

func TestStableLayerIndicesAfterReorder(t *testing.T) {
	s := NewScene()
	for i := 0; i < 150; i++ {
		s.Elements = append(s.Elements, NewElement(KindRectangle, float64(i), 0, 10, 10))
	}
	s.Reorder([]string{s.Elements[0].ID}, "front")
	raw, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	previous := ""
	for _, e := range got.Elements {
		if !ValidOrderKey(e.Index) || e.Index <= previous {
			t.Fatalf("invalid exported order: %q after %q", e.Index, previous)
		}
		previous = e.Index
	}
}

func TestNestedExtensionsAndLegacyBinding(t *testing.T) {
	s, err := Decode(`{"type":"excalidraw","version":2,"elements":[{"id":"r","type":"rectangle","x":0,"y":0,"width":100,"height":100,"roundness":{"type":3,"vendor":7}},{"id":"a","type":"arrow","x":100,"y":50,"width":100,"height":0,"points":[[0,0],[100,0]],"startBinding":{"elementId":"r","focus":0,"gap":0,"extension":{"keep":true}}}],"files":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if b := s.ElementByID("a").StartBinding; b.Mode == "" || b.FixedPoint != (Point{1, .5}) {
		t.Fatalf("legacy binding was not restored: %+v", b)
	}
	raw, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"vendor": 7`) || !strings.Contains(raw, `"keep": true`) {
		t.Fatal("nested extension fields lost")
	}
}
