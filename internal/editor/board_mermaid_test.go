package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

func TestMermaidCreatesEditableBoundElements(t *testing.T) {
	for _, src := range []string{"flowchart LR\n A[Client] -->|request| B[(Database)]", "sequenceDiagram\n participant A as Client\n participant B as Server\n A->>B: request\n B-->>A: response"} {
		s, err := MermaidBoard(src, whiteboard.Point{500, 300})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(s.PlainText(), "Client") || !strings.Contains(s.PlainText(), "request") {
			t.Fatal("Mermaid labels lost")
		}
		arrows := 0
		for _, e := range s.Elements {
			if e.Kind == whiteboard.KindArrow {
				arrows++
			}
		}
		if arrows == 0 {
			t.Fatal("connections are not editable arrows")
		}
	}
	if _, err := MermaidBoard("pie\n title unsupported", whiteboard.Point{}); err == nil {
		t.Fatal("unsupported Mermaid silently accepted")
	}
}

// A diamond's bound text only gets the inscribed half width; conversion must
// widen the shape so short decision labels stay on one line and edges follow.
func TestMermaidDiamondLabelDoesNotWrap(t *testing.T) {
	s, err := MermaidBoard("flowchart LR\n A[客户端] --> B{API 网关}\n B --> C[(数据库)]", whiteboard.Point{500, 300})
	if err != nil {
		t.Fatal(err)
	}
	var diamond *whiteboard.Element
	for _, e := range s.Elements {
		if e.Kind == whiteboard.KindDiamond {
			diamond = e
		}
	}
	if diamond == nil {
		t.Fatal("no diamond")
	}
	label := s.ElementByID(diamond.BoundTextID())
	if label == nil || strings.Contains(label.Text, "\n") {
		t.Fatalf("diamond label wrapped: %+v", label)
	}
	for _, e := range s.Elements {
		if e.Kind != whiteboard.KindArrow || e.EndBinding == nil || e.EndBinding.ElementID != diamond.ID {
			continue
		}
		end := whiteboard.Add(whiteboard.Point{e.X, e.Y}, e.Points[len(e.Points)-1])
		if end[0] > diamond.X+2 {
			t.Fatalf("incoming edge ends inside the widened diamond: %v vs x=%v", end, diamond.X)
		}
	}
}
