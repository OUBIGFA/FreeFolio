package whiteboard

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// link connects from into a node's slot, or its first free slot of from's
// type when slot is "".
func link(t *testing.T, s *Scene, from, to *Element, slot string) {
	t.Helper()
	n := AINodeOf(to)
	if slot == "" {
		sl, ok := AIFindInputByType(n, s.AISourceType(from))
		if !ok {
			t.Fatalf("no slot for %s in %s", s.AISourceType(from), n.Role)
		}
		slot = sl.Name
	}
	n.Connect(slot, from.ID)
	if err := SetAINode(to, n); err != nil {
		t.Fatal(err)
	}
}

func slotNames(n *AINode) []string {
	var out []string
	for _, sl := range AISlots(n) {
		out = append(out, sl.Name)
	}
	return out
}

func TestAIValidConnectionMatchesLiteGraph(t *testing.T) {
	for _, c := range []struct {
		a, b string
		ok   bool
	}{
		{"STRING", "STRING", true}, {"string", "STRING", true}, {"IMAGE", "STRING", false},
		{"*", "IMAGE", true}, {"", "IMAGE", true}, {"IMAGE,STRING", "string", true}, {"IMAGE,VIDEO", "STRING", false},
	} {
		if got := AIValidConnection(c.a, c.b); got != c.ok {
			t.Errorf("%q→%q = %v", c.a, c.b, got)
		}
	}
}

func TestAISlotsGrowReplaceAndClose(t *testing.T) {
	n := &AINode{Role: AIImage}
	if got := slotNames(n); !slices.Equal(got, []string{"prompt", "image.0", "mask"}) {
		t.Fatal(got)
	}
	// A dropped picture goes to the first free picture slot, not the mask.
	sl, _ := AIFindInputByType(n, AITypeImage)
	n.Connect(sl.Name, "a")
	sl, _ = AIFindInputByType(n, AITypeImage)
	n.Connect(sl.Name, "b")
	if got := slotNames(n); !slices.Equal(got, []string{"prompt", "image.0", "image.1", "image.2", "mask"}) {
		t.Fatal(got)
	}
	// An input holds one link: connecting again replaces it.
	n.Connect("image.0", "c")
	if n.LinkAt("image.0") != "c" || len(n.Links) != 2 {
		t.Fatalf("%+v", n.Links)
	}
	// Removing a list member closes the gap.
	n.Disconnect("image.0")
	if n.LinkAt("image.0") != "b" || n.LinkAt("image.1") != "" || !slices.Equal(slotNames(n), []string{"prompt", "image.0", "image.1", "mask"}) {
		t.Fatalf("%+v %v", n.Links, slotNames(n))
	}
	// With every typed slot taken, findFreeSlotOfType falls back to the
	// first occupied slot of the type.
	v := &AINode{Role: AIVideo}
	v.Connect("image", "x")
	if sl, ok := AIFindInputByType(v, AITypeImage); !ok || sl.Name != "image" {
		t.Fatal(sl, ok)
	}
	if _, ok := AIFindInputByType(&AINode{Role: AIConcat}, AITypeImage); ok {
		t.Fatal("concat took a picture")
	}
	if sl, ok := AIFindInputByType(&AINode{Role: AIText}, AITypeString); !ok || sl.Name != "prompt" {
		t.Fatal("text goes to the instruction slot first", sl)
	}
}

func TestAIInputsFollowSlots(t *testing.T) {
	s := NewScene()
	style := textCard(s, "赛博朋克风格")
	img := NewElement(KindImage, 0, 0, 50, 50)
	img.FileID = "f1"
	frame := NewElement(KindFrame, 0, 0, 300, 300)
	note := NewElement(KindNote, 10, 10, 50, 50)
	note.FrameID = frame.ID
	noteText := NewElement(KindText, 10, 10, 0, 0)
	noteText.Text, noteText.OriginalText = "换成陶瓷", "换成陶瓷"
	linked := NewElement(KindEllipse, 0, 0, 40, 40)
	linked.Link = "[[产品说明|说明]]"
	up := NewAINode(AIImage, 0, 0)
	node := NewAINode(AIText, 400, 0)
	arrow := NewElement(KindArrow, 0, 0, 10, 10)
	s.Elements = append(s.Elements, img, frame, note, noteText, linked, up, node, arrow)
	_ = s.BindText(noteText.ID, note.ID, nil)
	if s.AISourceType(arrow) != "" || s.AISourceType(frame) != AITypeImage || s.AISourceType(style) != AITypeString {
		t.Fatal("source types")
	}
	link(t, s, style, node, "prompt")
	link(t, s, linked, node, "")
	link(t, s, img, node, "")
	link(t, s, frame, node, "")
	link(t, s, up, node, "")
	ins := s.AIInputs(node.ID)
	var got []string
	for _, in := range ins {
		got = append(got, in.Slot+":"+in.Kind)
	}
	if !slices.Equal(got, []string{"prompt:text", "text.0:note", "image.0:image", "image.1:frame", "image.2:node"}) {
		t.Fatal(got)
	}
	if ins[0].Group != "prompt" || ins[0].Text != "赛博朋克风格" || ins[1].Note != "产品说明" || ins[2].FileID != "f1" || ins[3].Text != "换成陶瓷" {
		t.Fatalf("%+v", ins)
	}
	if deps := s.AIDependencies(); !slices.Equal(deps[node.ID], []string{up.ID}) {
		t.Fatal(deps)
	}
	if !s.AICreatesCycle(node.ID, up.ID) || s.AICreatesCycle(up.ID, node.ID) || !s.AICreatesCycle(node.ID, node.ID) {
		t.Fatal("cycle check")
	}
	// A deleted source is not an input; undoing the delete brings it back.
	img.IsDeleted = true
	if len(s.AIInputs(node.ID)) != 4 {
		t.Fatal("deleted source still read")
	}
	img.IsDeleted = false
	// Arrows are drawings, never inputs.
	if !s.Bind(arrow.ID, true, node.ID, ElementBounds(node).Center()) || len(s.AIInputs(node.ID)) != 5 {
		t.Fatal("an arrow changed the inputs")
	}
}

func TestPasteRemapsNodeLinks(t *testing.T) {
	s := NewScene()
	a, b := NewAINode(AIPrompt, 0, 0), NewAINode(AIImage, 400, 0)
	outside := textCard(s, "外部")
	s.Elements = append(s.Elements, a, b)
	link(t, s, a, b, "prompt")
	link(t, s, outside, b, "image.0")
	n := AINodeOf(b)
	n.Connect("mask", outside.ID)
	_ = SetAINode(b, n)
	clip, err := s.Clipboard([]string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := s.Paste(clip, Point{0, 600})
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	copyB := AINodeOf(s.ElementByID(ids[1]))
	if copyB.LinkAt("prompt") != ids[0] || len(copyB.Links) != 1 {
		t.Fatalf("pasted links %+v", copyB.Links)
	}
	data, err := ExportCanvas(s)
	if err != nil || !strings.Contains(string(data), `"fromNode": "`+a.ID+`"`) {
		t.Fatal(string(data), err)
	}
	var out canvasFile
	_ = json.Unmarshal(data, &out)
	if len(out.Edges) != 4 {
		t.Fatalf("edges %+v", out.Edges)
	}
}
