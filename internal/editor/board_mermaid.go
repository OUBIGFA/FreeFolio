package editor

import (
	"math"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/mermaid"
	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

// MermaidBoard reuses FreeFolio's parser/layout and emits standard editable
// primitives. Source code remains owned by the document that invoked conversion.
func MermaidBoard(src string, at whiteboard.Point) (*whiteboard.Scene, error) {
	d, err := mermaid.ParseAndLayout(src, func(text string, size float32, bold bool) (float32, float32) {
		return float32(boardMeasure(text, whiteboard.Font{Size: float64(size), Family: 5})), size * 1.25
	})
	if err != nil {
		return nil, err
	}
	s := whiteboard.NewScene()
	var nodes, frames, edges []*whiteboard.Element
	for _, sh := range d.Shapes {
		if sh.W <= 1 || sh.H <= 1 {
			continue
		}
		x, y, w, h := float64(sh.X), float64(sh.Y), float64(sh.W), float64(sh.H)
		if sh.Role == mermaid.RoleSubgraph || sh.Role == mermaid.RoleFragment {
			f := whiteboard.NewElement(whiteboard.KindFrame, x, y, w, h)
			s.Elements = append(s.Elements, f)
			frames = append(frames, f)
			continue
		}
		kind := "rect"
		switch sh.Kind {
		case mermaid.ShapeRound, mermaid.ShapeStadium:
			kind = "round"
		case mermaid.ShapeCylinder:
			kind = "cylinder"
		case mermaid.ShapeCircle:
			kind = "circle"
		case mermaid.ShapeDiamond:
			kind = "diamond"
		case mermaid.ShapeHexagon, mermaid.ShapePentagon:
			kind = "hexagon"
		case mermaid.ShapeParallelogram:
			kind = "parallelogram"
		case mermaid.ShapeActor:
			kind = "actor"
		}
		es := whiteboard.DiagramShape(kind, x, y, w, h)
		for _, e := range es {
			e.FillColor = "#a5d8ff"
			e.FillStyle = whiteboard.FillHachure
			if sh.Role == mermaid.RoleNote {
				e.FillColor = "#ffdf6b"
			}
			if sh.Dashed {
				e.StrokeStyle = whiteboard.StrokeDashed
			}
		}
		nodes = append(nodes, es[0])
		s.Elements = append(s.Elements, es...)
	}
	marker := func(m mermaid.MarkerKind) whiteboard.Arrowhead {
		switch m {
		case mermaid.MarkerArrow:
			return whiteboard.ArrowTriangle
		case mermaid.MarkerOpenArrow:
			return whiteboard.ArrowOpen
		case mermaid.MarkerCircle:
			return whiteboard.ArrowCircle
		case mermaid.MarkerCross:
			return whiteboard.ArrowBar
		}
		return whiteboard.ArrowNone
	}
	for _, path := range d.Paths {
		if len(path.Points) < 2 {
			continue
		}
		first, last := path.Points[0], path.Points[len(path.Points)-1]
		kind := whiteboard.KindArrow
		if path.StartMarker == mermaid.MarkerNone && path.EndMarker == mermaid.MarkerNone {
			kind = whiteboard.KindLine
		}
		e := whiteboard.NewElement(kind, float64(first[0]), float64(first[1]), float64(last[0]-first[0]), float64(last[1]-first[1]))
		e.Points = nil
		for _, p := range path.Points {
			e.Points = append(e.Points, whiteboard.Point{float64(p[0] - first[0]), float64(p[1] - first[1])})
		}
		if !path.Smooth {
			e.Roundness = nil
		}
		e.StartArrowhead, e.EndArrowhead = marker(path.StartMarker), marker(path.EndMarker)
		if path.Style == mermaid.EdgeDashed {
			e.StrokeStyle = whiteboard.StrokeDashed
		} else if path.Style == mermaid.EdgeThick {
			e.StrokeWidth = 3.2
		}
		s.Elements = append(s.Elements, e)
		edges = append(edges, e)
		if d.Kind == "flowchart" {
			for i, p := range []whiteboard.Point{{float64(first[0]), float64(first[1])}, {float64(last[0]), float64(last[1])}} {
				for _, n := range nodes {
					if whiteboard.HitTest(n, p, 18) {
						s.Bind(e.ID, i == 1, n.ID, p)
						break
					}
				}
			}
		}
	}
	for _, lb := range d.Labels {
		text := strings.TrimSpace(lb.Text)
		if text == "" {
			continue
		}
		e := whiteboard.NewElement(whiteboard.KindText, float64(lb.X), float64(lb.Y), float64(lb.W), float64(lb.H))
		e.Text, e.OriginalText = text, text
		e.FontSize = math.Max(12, float64(lb.Size))
		e.TextAlign = "center"
		s.Elements = append(s.Elements, e)
		center := whiteboard.Point{float64(lb.X + lb.W/2), float64(lb.Y + lb.H/2)}
		var host *whiteboard.Element
		best := math.Inf(1)
		if lb.Role == mermaid.RoleEdgeLabel {
			for _, edge := range edges {
				dist := whiteboard.Distance(whiteboard.ElementBounds(edge).Center(), center)
				if dist < best && dist < 80 && edge.BoundTextID() == "" {
					best = dist
					host = edge
				}
			}
		} else {
			for _, node := range nodes {
				bounds := whiteboard.ElementBounds(node)
				if bounds.Contains(center) && bounds.W*bounds.H < best && node.BoundTextID() == "" {
					best = bounds.W * bounds.H
					host = node
				}
			}
		}
		if host != nil && host.Kind == whiteboard.KindDiamond {
			// Mermaid sizes a diamond around its label, but bound text in a
			// diamond only gets the inscribed half width, so short labels such
			// as "API 网关" would wrap mid-word. Widen around the centre and let
			// bound edges follow the new tips.
			lw := 0.0
			for _, line := range strings.Split(text, "\n") {
				lw = math.Max(lw, boardMeasure(line, whiteboard.Font{Size: e.FontSize, Family: e.FontFamily}))
			}
			if need := 2*(lw+10) + 4; need > host.W {
				host.X -= (need - host.W) / 2
				host.W = need
				s.UpdateBindings(map[string]bool{host.ID: true})
			}
		}
		if host != nil {
			_ = s.BindText(e.ID, host.ID, boardMeasure)
		} else {
			s.LayoutText(e.ID, boardMeasure)
		}
	}
	for _, e := range s.Elements {
		if e.IsFrame() {
			continue
		}
		for _, f := range frames {
			if whiteboard.ElementBounds(f).Contains(whiteboard.ElementBounds(e).Center()) {
				e.FrameID = f.ID
				break
			}
		}
	}
	s.RepairBindings()
	center := whiteboard.SceneBounds(s).Center()
	delta := whiteboard.Sub(at, center)
	for _, e := range s.Elements {
		e.X += delta[0]
		e.Y += delta[1]
	}
	return s, nil
}
