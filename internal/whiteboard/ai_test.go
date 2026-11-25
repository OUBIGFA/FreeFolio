package whiteboard

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

func connect(t *testing.T, s *Scene, from, to *Element, label string) *Element {
	t.Helper()
	a := NewElement(KindArrow, from.X, from.Y, 10, 10)
	s.Elements = append(s.Elements, a)
	if !s.Bind(a.ID, false, from.ID, ElementBounds(from).Center()) || !s.Bind(a.ID, true, to.ID, ElementBounds(to).Center()) {
		t.Fatal("bind failed")
	}
	if label != "" {
		l := NewElement(KindText, 0, 0, 0, 0)
		l.Text, l.OriginalText = label, label
		s.Elements = append(s.Elements, l)
		if err := s.BindText(l.ID, a.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func textCard(s *Scene, text string) *Element {
	r := NewElement(KindRectangle, 0, 0, 100, 60)
	l := NewElement(KindText, 0, 0, 0, 0)
	l.Text, l.OriginalText = text, text
	s.Elements = append(s.Elements, r, l)
	_ = s.BindText(l.ID, r.ID, nil)
	return r
}

func TestAINodeRoundTripKeepsOtherCustomData(t *testing.T) {
	e := NewAINode(AIImage, 10, 20)
	n := AINodeOf(e)
	if n == nil || n.Role != AIImage || e.W != 360 {
		t.Fatalf("%+v", n)
	}
	e.CustomData = json.RawMessage(`{"other":{"keep":true},"freefolioAI":{"v":1,"role":"text"}}`)
	n = AINodeOf(e)
	n.Instruction = "总结"
	n.AddOutputs([]AIOutput{{Kind: "text", Text: "a"}, {Kind: "text", Text: "b"}})
	if err := SetAINode(e, n); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(e.CustomData), `"keep":true`) {
		t.Fatal("lost other namespace")
	}
	s := NewScene()
	s.Elements = append(s.Elements, e)
	text, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(text)
	if err != nil {
		t.Fatal(err)
	}
	got := AINodeOf(back.Elements[0])
	if got.Instruction != "总结" || got.Output().Text != "a" || got.Runs != 1 || got.Outputs[1].Run != 1 {
		t.Fatalf("%+v", got)
	}
	got.RemoveOutput(0)
	if got.Output().Text != "b" {
		t.Fatal("remove")
	}
	e.CustomData = json.RawMessage(`{"freefolioAI":{"v":2,"role":"text"}}`)
	if IsAINode(e) {
		t.Fatal("future node treated as known")
	}
	if summary := back.MarkdownSummary(); len(summary) != 1 || !strings.Contains(summary[0].Text, "总结") {
		t.Fatalf("%+v", summary)
	}
}

func TestAIPlanOrdersAndDetectsCycles(t *testing.T) {
	s := NewScene()
	a, b, c, d := NewAINode(AIText, 0, 0), NewAINode(AIText, 0, 0), NewAINode(AIImage, 0, 0), NewAINode(AIText, 0, 0)
	s.Elements = append(s.Elements, c, b, a, d)
	link(t, s, a, b, "")
	link(t, s, b, c, "")
	order, deps, err := s.AIPlan(nil, false)
	if err != nil || len(order) != 4 || slices.Index(order, a.ID) > slices.Index(order, b.ID) || slices.Index(order, b.ID) > slices.Index(order, c.ID) || !slices.Equal(deps[c.ID], []string{b.ID}) {
		t.Fatalf("%v %v %v", order, deps, err)
	}
	order, _, _ = s.AIPlan([]string{c.ID}, true)
	if !slices.Equal(order, []string{a.ID, b.ID, c.ID}) {
		t.Fatalf("upstream %v", order)
	}
	order, deps, _ = s.AIPlan([]string{c.ID}, false)
	if !slices.Equal(order, []string{c.ID}) || len(deps[c.ID]) != 0 {
		t.Fatalf("single %v %v", order, deps)
	}
	link(t, s, c, a, "")
	if _, _, err := s.AIPlan(nil, false); !errors.Is(err, ErrAICycle) {
		t.Fatal(err)
	}
}

func TestRunAIFlowBlocksDownstreamOfFailures(t *testing.T) {
	deps := map[string][]string{"b": {"a"}, "c": {"b"}, "d": nil}
	var mu sync.Mutex
	ran := map[string]bool{}
	results := map[string]error{}
	RunAIFlow(context.Background(), []string{"a", "b", "c", "d"}, deps, 2, func(_ context.Context, id string) error {
		mu.Lock()
		ran[id] = true
		mu.Unlock()
		if id == "b" {
			return errors.New("boom")
		}
		return nil
	}, func(id string, err error) { mu.Lock(); results[id] = err; mu.Unlock() })
	if ran["c"] || !ran["a"] || !ran["d"] || !errors.Is(results["c"], ErrAIUpstream) || results["a"] != nil || len(results) != 4 {
		t.Fatalf("%v %v", ran, results)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results = map[string]error{}
	RunAIFlow(ctx, []string{"a"}, nil, 1, func(context.Context, string) error { t.Fatal("ran"); return nil }, func(id string, err error) { results[id] = err })
	if !errors.Is(results["a"], context.Canceled) {
		t.Fatal(results)
	}
}

func TestAIHashAndConcat(t *testing.T) {
	n := &AINode{Role: AIText, Instruction: "x"}
	h := AIHash(n, []string{"a", "b"})
	if h != AIHash(n, []string{"a", "b"}) || h == AIHash(n, []string{"ab"}) || h == AIHash(&AINode{Role: AIText, Instruction: "y"}, []string{"a", "b"}) {
		t.Fatal("hash")
	}
	if got := ConcatTemplate("{1}, {2}; {all}", []string{"red {2}", "cat"}); got != "red {2}, cat; red {2}\ncat" {
		t.Fatal(got)
	}
}

func TestJSONCanvasRoundTrip(t *testing.T) {
	s := NewScene()
	a := textCard(s, "Idea")
	b := NewElement(KindRectangle, 300, 0, 100, 60)
	b.Link = "[[Notes]]"
	frame := NewElement(KindFrame, -50, -50, 600, 300)
	frame.Name = "Group"
	s.Elements = append(s.Elements, b, frame)
	connect(t, s, a, b, "next")
	data, err := ExportCanvas(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"file": "Notes.md"`) || !strings.Contains(string(data), `"label": "next"`) || !strings.Contains(string(data), `"type": "group"`) {
		t.Fatal(string(data))
	}
	back, err := ImportCanvas(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	var arrows, links, frames int
	for _, e := range back.Elements {
		switch {
		case e.Kind == KindArrow && e.StartBinding != nil && e.EndBinding != nil && back.ElementText(back.ElementByID(e.BoundTextID())) == "next":
			arrows++
		case e.Link == "[[Notes]]":
			links++
		case e.IsFrame() && e.Name == "Group":
			frames++
		}
	}
	if arrows != 1 || links != 1 || frames != 1 {
		t.Fatalf("%d %d %d", arrows, links, frames)
	}
	if _, err := ImportCanvas([]byte(`{"nodes":[{"id":"x","type":"weird"}]}`), nil); err == nil {
		t.Fatal("unknown node type accepted")
	}
}
