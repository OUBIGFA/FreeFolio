package whiteboard

import (
	"encoding/json"
	"fmt"
	"math"
	"path"
	"strings"
)

// JSON Canvas 1.0 (jsoncanvas.org, MIT) is Obsidian's open file of an
// infinite canvas: nodes (text, file, link, group) and edges between their
// sides. It exchanges a board's layout, text and connections with other
// tools; hand-drawn paths and styles stay in the Excalidraw file.

type canvasFile struct {
	Nodes []canvasNode `json:"nodes"`
	Edges []canvasEdge `json:"edges"`
}

type canvasNode struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Color  string  `json:"color,omitempty"`
	Text   string  `json:"text,omitempty"`
	File   string  `json:"file,omitempty"`
	URL    string  `json:"url,omitempty"`
	Label  string  `json:"label,omitempty"`
}

type canvasEdge struct {
	ID       string `json:"id"`
	FromNode string `json:"fromNode"`
	FromSide string `json:"fromSide,omitempty"`
	FromEnd  string `json:"fromEnd,omitempty"`
	ToNode   string `json:"toNode"`
	ToSide   string `json:"toSide,omitempty"`
	ToEnd    string `json:"toEnd,omitempty"`
	Color    string `json:"color,omitempty"`
	Label    string `json:"label,omitempty"`
}

// canvasColors are the spec's preset colors, by number.
var canvasColors = map[string]string{"1": "#ffc9c9", "2": "#ffd8a8", "3": "#fff3bf", "4": "#b2f2bb", "5": "#99e9f2", "6": "#d0bfff"}

func canvasColor(fill string) string {
	if fill == "" || fill == "transparent" || fill == "#ffffff" {
		return ""
	}
	for k, v := range canvasColors {
		if strings.EqualFold(v, fill) {
			return k
		}
	}
	return fill
}

func canvasSide(b *Binding) string {
	if b == nil {
		return ""
	}
	x, y := b.FixedPoint[0]-.5, b.FixedPoint[1]-.5
	if math.Abs(x) >= math.Abs(y) {
		if x < 0 {
			return "left"
		}
		return "right"
	}
	if y < 0 {
		return "top"
	}
	return "bottom"
}

// ExportCanvas writes the board as JSON Canvas.
func ExportCanvas(s *Scene) ([]byte, error) {
	out := canvasFile{Nodes: []canvasNode{}, Edges: []canvasEdge{}}
	exported := map[string]bool{}
	for _, e := range s.Elements {
		if e.IsDeleted || e.IsLinear() || e.Kind == KindFreedraw || e.Kind == KindText && e.ContainerID != "" {
			continue
		}
		n := canvasNode{ID: e.ID, Type: "text", X: math.Round(e.X), Y: math.Round(e.Y), Width: math.Round(math.Max(1, e.W)), Height: math.Round(math.Max(1, e.H)), Color: canvasColor(e.FillColor)}
		switch {
		case e.IsFrame():
			n.Type, n.Label, n.Color = "group", e.Name, ""
		case e.Kind == KindImage:
			f := s.Files[e.FileID]
			if f == nil || f.Path == "" {
				continue
			}
			n.Type, n.File = "file", f.Path
		case IsAINode(e):
			a := AINodeOf(e)
			parts := []string{a.Title, a.Instruction}
			if o := a.Output(); o != nil && o.Kind == "text" {
				parts = append(parts, o.Text)
			}
			if o := a.Output(); o != nil && o.Path != "" {
				n.Type, n.File = "file", o.Path
				break
			}
			n.Text = strings.TrimSpace(strings.Join(parts, "\n\n"))
		case WikiTarget(e.Link) != "" && strings.TrimSpace(s.ElementText(e)) == "":
			n.Type, n.File = "file", WikiTarget(e.Link)+".md"
		case strings.Contains(e.Link, "://") && strings.TrimSpace(s.ElementText(e)) == "":
			n.Type, n.URL = "link", e.Link
		default:
			n.Text = s.ElementText(e)
		}
		out.Nodes = append(out.Nodes, n)
		exported[e.ID] = true
	}
	for _, a := range s.Elements {
		if a.IsDeleted || !a.IsLinear() || a.StartBinding == nil || a.EndBinding == nil || !exported[a.StartBinding.ElementID] || !exported[a.EndBinding.ElementID] {
			continue
		}
		edge := canvasEdge{ID: a.ID, FromNode: a.StartBinding.ElementID, FromSide: canvasSide(a.StartBinding), ToNode: a.EndBinding.ElementID, ToSide: canvasSide(a.EndBinding), Label: s.ElementText(s.ElementByID(a.BoundTextID()))}
		// The spec's defaults are an arrow at the end and none at the start.
		if a.EndArrowhead == "" {
			edge.ToEnd = "none"
		}
		if a.StartArrowhead != "" {
			edge.FromEnd = "arrow"
		}
		out.Edges = append(out.Edges, edge)
	}
	return json.MarshalIndent(out, "", "\t")
}

// sidePoint is where an edge meets a box.
func sidePoint(b Box, side string) Point {
	switch side {
	case "left":
		return Point{b.X, b.Y + b.H/2}
	case "right":
		return Point{b.X + b.W, b.Y + b.H/2}
	case "top":
		return Point{b.X + b.W/2, b.Y}
	case "bottom":
		return Point{b.X + b.W/2, b.Y + b.H}
	}
	return b.Center()
}

// ImportCanvas reads JSON Canvas into a new scene: cards with their text,
// files and links as linked cards, groups as frames and edges as bound
// arrows with their labels.
func ImportCanvas(data []byte, m MeasureFunc) (*Scene, error) {
	if len(data) > MaxSceneBytes {
		return nil, fmt.Errorf("whiteboard: canvas exceeds size limit")
	}
	var in canvasFile
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("whiteboard: invalid JSON Canvas: %w", err)
	}
	s := NewScene()
	ids := map[string]string{}
	label := func(container *Element, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		t := NewElement(KindText, container.X, container.Y, 0, 0)
		t.FontSize = 16
		t.Text, t.OriginalText = text, text
		s.Elements = append(s.Elements, t)
		_ = s.BindText(t.ID, container.ID, m)
	}
	var frames []*Element
	for _, n := range in.Nodes {
		if n.Type != "group" {
			continue
		}
		f := NewElement(KindFrame, n.X, n.Y, n.Width, n.Height)
		f.Name = n.Label
		s.Elements = append(s.Elements, f)
		frames = append(frames, f)
		ids[n.ID] = f.ID
	}
	for _, n := range in.Nodes {
		var e *Element
		switch n.Type {
		case "group":
			continue
		case "text", "file", "link":
			e = NewElement(KindRectangle, n.X, n.Y, math.Max(n.Width, 20), math.Max(n.Height, 20))
			e.Roughness = 0
		default:
			return nil, fmt.Errorf("whiteboard: unsupported canvas node type %q", n.Type)
		}
		if c := canvasColors[n.Color]; c != "" {
			e.FillColor, e.FillStyle = c, FillSolid
		} else if strings.HasPrefix(n.Color, "#") {
			e.FillColor, e.FillStyle = n.Color, FillSolid
		}
		s.Elements = append(s.Elements, e)
		ids[n.ID] = e.ID
		switch n.Type {
		case "text":
			label(e, n.Text)
		case "file":
			name := strings.TrimSuffix(n.File, ".md")
			e.Link = "[[" + name + "]]"
			label(e, path.Base(n.File))
		case "link":
			e.Link = n.URL
			label(e, n.URL)
		}
		for _, f := range frames {
			if (Box{X: f.X, Y: f.Y, W: f.W, H: f.H}).Contains(ElementBounds(e).Center()) {
				e.FrameID = f.ID
			}
		}
	}
	for _, edge := range in.Edges {
		from, to := s.ElementByID(ids[edge.FromNode]), s.ElementByID(ids[edge.ToNode])
		if from == nil || to == nil {
			return nil, fmt.Errorf("whiteboard: canvas edge %q joins unknown nodes", edge.ID)
		}
		start, end := sidePoint(ElementBounds(from), edge.FromSide), sidePoint(ElementBounds(to), edge.ToSide)
		a := NewElement(KindArrow, start[0], start[1], 0, 0)
		a.Points = []Point{{0, 0}, Sub(end, start)}
		a.W, a.H = math.Abs(end[0]-start[0]), math.Abs(end[1]-start[1])
		a.EndArrowhead = ArrowOpen
		if edge.ToEnd == "none" {
			a.EndArrowhead = ArrowNone
		}
		if edge.FromEnd == "arrow" {
			a.StartArrowhead = ArrowOpen
		}
		s.Elements = append(s.Elements, a)
		s.Bind(a.ID, false, from.ID, start)
		s.Bind(a.ID, true, to.ID, end)
		label(a, edge.Label)
	}
	return s, s.Validate()
}
