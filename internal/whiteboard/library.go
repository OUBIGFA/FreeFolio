package whiteboard

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

type LibraryItem struct {
	ID       string               `json:"id"`
	Name     string               `json:"name,omitempty"`
	Category string               `json:"category,omitempty"`
	Status   string               `json:"status,omitempty"`
	Created  int64                `json:"created"`
	Elements []*Element           `json:"elements"`
	Files    map[string]*FileData `json:"files,omitempty"`
}
type libraryFile struct {
	Type    string        `json:"type"`
	Version int           `json:"version"`
	Source  string        `json:"source"`
	Items   []LibraryItem `json:"libraryItems"`
	Legacy  [][]*Element  `json:"library,omitempty"`
}

func DecodeLibrary(src string) ([]LibraryItem, error) {
	if len(src) > MaxSceneBytes {
		return nil, fmt.Errorf("whiteboard: library exceeds size limit")
	}
	var file libraryFile
	if err := json.Unmarshal([]byte(src), &file); err != nil {
		return nil, err
	}
	if file.Type != "excalidrawlib" || file.Version < 1 || file.Version > 2 {
		return nil, fmt.Errorf("whiteboard: unsupported library")
	}
	if file.Version == 1 {
		for _, es := range file.Legacy {
			file.Items = append(file.Items, LibraryItem{ID: NewID(), Elements: es})
		}
	}
	for i := range file.Items {
		it := &file.Items[i]
		if it.ID == "" {
			it.ID = NewID()
		}
		s := NewScene()
		s.Elements = it.Elements
		if it.Files != nil {
			s.Files = it.Files
		}
		if err := s.Validate(); err != nil {
			return nil, err
		}
	}
	return file.Items, nil
}
func EncodeLibrary(items []LibraryItem) (string, error) {
	for _, it := range items {
		s := NewScene()
		s.Elements = it.Elements
		s.Files = it.Files
		if err := s.Validate(); err != nil {
			return "", err
		}
	}
	b, err := json.MarshalIndent(libraryFile{Type: "excalidrawlib", Version: 2, Source: "https://github.com/OUBIGFA/FreeFolio", Items: items}, "", "  ")
	return string(b) + "\n", err
}
func (s *Scene) InsertLibrary(it LibraryItem, at Point) ([]string, error) {
	from := NewScene()
	from.Elements = it.Elements
	from.Files = it.Files
	var ids []string
	for _, e := range it.Elements {
		ids = append(ids, e.ID)
	}
	clip, err := from.Clipboard(ids)
	if err != nil {
		return nil, err
	}
	ids, err = s.Paste(clip, at)
	if err == nil {
		s.Group(ids)
	}
	return ids, err
}
func (s *Scene) LibrarySelection(ids []string, name, category string) (LibraryItem, error) {
	clip, err := s.Clipboard(ids)
	if err != nil {
		return LibraryItem{}, err
	}
	var c clipboardData
	if err = json.Unmarshal([]byte(clip), &c); err != nil {
		return LibraryItem{}, err
	}
	return LibraryItem{ID: NewID(), Name: name, Category: category, Status: "unpublished", Elements: c.Elements, Files: c.Files}, nil
}

// DiagramShape constructs complex shapes from standard Excalidraw objects.
// Other editors see the same geometry without understanding FreeFolio metadata.
func DiagramShape(kind string, x, y, w, h float64) []*Element {
	base := func(k ElementKind) *Element { return NewElement(k, x, y, w, h) }
	poly := func(ps []Point) *Element {
		e := base(KindLine)
		e.Points = ps
		e.Polygon = true
		e.Roundness = nil
		e.Points = append(e.Points, e.Points[0])
		return e
	}
	var out []*Element
	switch kind {
	case "cylinder":
		r := h * .12
		body := poly([]Point{{0, r}, {w, r}, {w, h - r}, {0, h - r}})
		body.FillColor = "#a5d8ff"
		body.FillStyle = FillSolid
		top := NewElement(KindEllipse, x, y, w, 2*r)
		bottom := NewElement(KindEllipse, x, y+h-2*r, w, 2*r)
		top.FillColor = "#a5d8ff"
		top.FillStyle = FillSolid
		out = []*Element{body, bottom, top}
	case "cloud":
		var ps []Point
		for i := 0; i < 80; i++ {
			a := float64(i) * 2 * math.Pi / 80
			k := 1 + .13*math.Sin(5*a)
			ps = append(ps, Point{w/2 + w*.43*k*math.Cos(a), h/2 + h*.4*k*math.Sin(a)})
		}
		out = []*Element{poly(ps)}
	case "hexagon":
		out = []*Element{poly([]Point{{w * .2, 0}, {w * .8, 0}, {w, h / 2}, {w * .8, h}, {w * .2, h}, {0, h / 2}})}
	case "parallelogram":
		out = []*Element{poly([]Point{{w * .2, 0}, {w, 0}, {w * .8, h}, {0, h}})}
	case "triangle":
		out = []*Element{poly([]Point{{w / 2, 0}, {w, h}, {0, h}})}
	case "actor":
		head := NewElement(KindEllipse, x+w*.3, y, w*.4, h*.25)
		line := func(ps []Point) *Element { e := base(KindLine); e.Points = ps; e.Roundness = nil; return e }
		out = []*Element{head, line([]Point{{w / 2, h * .25}, {w / 2, h * .65}}), line([]Point{{w * .1, h * .45}, {w * .9, h * .45}}), line([]Point{{w * .1, h}, {w / 2, h * .65}, {w * .9, h}})}
	case "circle":
		out = []*Element{base(KindEllipse)}
	case "diamond":
		out = []*Element{base(KindDiamond)}
	default:
		e := base(KindRectangle)
		if kind == "rect" {
			e.Roundness = nil
		}
		out = []*Element{e}
	}
	if len(out) > 1 {
		g := NewID()
		for _, e := range out {
			e.GroupIDs = []string{g}
		}
	}
	return out
}

var builtinLibrary = sync.OnceValue(buildBuiltinLibrary)

func BuiltinLibrary() []LibraryItem { return builtinLibrary() }
func buildBuiltinLibrary() []LibraryItem {
	var items []LibraryItem
	makeItem := func(id, name, category string, build func(*Scene)) {
		s := NewScene()
		build(s)
		items = append(items, LibraryItem{ID: id, Name: name, Category: category, Status: "published", Elements: s.Elements, Files: s.Files})
	}
	label := func(s *Scene, text string, x, y, w, h float64) {
		e := NewElement(KindText, x, y, w, h)
		e.Text = text
		e.OriginalText = text
		e.FontSize = 18
		e.FontFamily = 5
		e.TextAlign = "center"
		s.Elements = append(s.Elements, e)
		s.LayoutText(e.ID, nil)
	}
	box := func(s *Scene, text string, x, y, w, h float64) *Element {
		e := NewElement(KindRectangle, x, y, w, h)
		e.FillColor = "#a5d8ff"
		s.Elements = append(s.Elements, e)
		t := NewElement(KindText, 0, 0, 0, 0)
		t.Text = text
		t.OriginalText = text
		s.Elements = append(s.Elements, t)
		_ = s.BindText(t.ID, e.ID, nil)
		return e
	}
	connect := func(s *Scene, a, b *Element) {
		p, q := Point{a.X + a.W, a.Y + a.H/2}, Point{b.X, b.Y + b.H/2}
		e := NewElement(KindArrow, p[0], p[1], q[0]-p[0], q[1]-p[1])
		s.Elements = append(s.Elements, e)
		s.Bind(e.ID, false, a.ID, p)
		s.Bind(e.ID, true, b.ID, q)
	}
	makeItem("database", "数据库集群", "架构", func(s *Scene) {
		for i := 0; i < 3; i++ {
			x := float64(i) * 145
			s.Elements = append(s.Elements, DiagramShape("cylinder", x, 0, 110, 110)...)
			label(s, fmt.Sprintf("DB %d", i+1), x+25, 45, 60, 25)
		}
	})
	makeItem("queue", "消息队列", "架构", func(s *Scene) {
		a := box(s, "Producer", 0, 30, 120, 70)
		b := box(s, "Queue", 180, 30, 160, 70)
		c := box(s, "Consumer", 400, 30, 120, 70)
		connect(s, a, b)
		connect(s, b, c)
	})
	makeItem("cloud", "云服务", "架构", func(s *Scene) {
		s.Elements = append(s.Elements, DiagramShape("cloud", 0, 0, 260, 170)...)
		label(s, "Cloud service", 50, 65, 160, 30)
	})
	makeItem("three-tier", "三层架构", "架构", func(s *Scene) {
		a := box(s, "Client", 0, 30, 130, 80)
		b := box(s, "Service", 200, 30, 150, 80)
		c := box(s, "Storage", 420, 30, 140, 80)
		connect(s, a, b)
		connect(s, b, c)
	})
	makeItem("browser", "浏览器线框", "界面", func(s *Scene) {
		box(s, "", 0, 0, 360, 240)
		box(s, "https://", 15, 12, 330, 35)
		box(s, "Sidebar", 15, 62, 90, 160)
		box(s, "Content", 120, 62, 225, 160)
	})
	makeItem("phone", "手机线框", "界面", func(s *Scene) { box(s, "", 0, 0, 170, 310); box(s, "App", 12, 25, 146, 255) })
	makeItem("swimlane", "泳道流程", "流程", func(s *Scene) {
		for i, name := range []string{"Client", "Service", "Storage"} {
			f := NewElement(KindFrame, 0, float64(i)*110, 450, 100)
			f.Name = name
			s.Elements = append(s.Elements, f)
			e := box(s, "Step", float64(i)*130+40, float64(i)*110+25, 100, 50)
			e.FrameID = f.ID
		}
	})
	makeItem("matrix", "四象限", "思考", func(s *Scene) {
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				box(s, fmt.Sprintf("%d", y*2+x+1), float64(x)*180, float64(y)*130, 170, 120)
			}
		}
	})
	makeItem("mindmap", "思维图", "思考", func(s *Scene) {
		center := box(s, "Topic", 0, 130, 120, 60)
		for i, name := range []string{"Idea A", "Idea B", "Idea C"} {
			e := box(s, name, 210, float64(i)*130, 140, 60)
			connect(s, center, e)
		}
	})
	makeItem("actor", "参与者", "流程", func(s *Scene) {
		s.Elements = append(s.Elements, DiagramShape("actor", 0, 0, 90, 160)...)
		label(s, "User", 10, 175, 70, 25)
	})
	makeItem("sequence", "服务调用", "流程", func(s *Scene) {
		a := box(s, "Client", 0, 0, 120, 60)
		b := box(s, "Server", 240, 0, 120, 60)
		connect(s, a, b)
	})
	makeItem("decision", "条件分支", "流程", func(s *Scene) {
		e := NewElement(KindDiamond, 0, 0, 170, 110)
		s.Elements = append(s.Elements, e)
		t := NewElement(KindText, 0, 0, 0, 0)
		t.Text = "Decision"
		t.OriginalText = t.Text
		s.Elements = append(s.Elements, t)
		_ = s.BindText(t.ID, e.ID, nil)
	})
	return items
}
