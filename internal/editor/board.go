package editor

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

type BoardTool uint8

const (
	ToolSelect BoardTool = iota
	ToolLasso
	ToolHand
	ToolRect
	ToolDiamond
	ToolEllipse
	ToolArrow
	ToolLine
	ToolFreedraw
	ToolText
	ToolImage
	ToolEraser
	ToolFrame
	ToolNote
)

type BoardOptions struct {
	// ImageAnnotation keeps the image inspector's tools and base image protected.
	ImageAnnotation bool
	// DismissImage handles Escape while inspecting, never while annotating.
	DismissImage      func()
	Theme             *ui.Theme
	Page              Page
	ReadOnly, Compact bool
	OnChange          func()
	Done              func()
	OpenLink          func(string)
	OpenWiki          func(string, bool)
	WikiSuggest       func(string) []string
	LinkedMentions    func() []string
	PickImage         func()
	DropFiles         func([]string)
	Export            func(string)
	History           func()
	Dispatch          func(func())
	ResolveFile       func(*whiteboard.FileData) ([]byte, error)
	UserLibrary       func() []whiteboard.LibraryItem
	SaveLibrary       func([]whiteboard.LibraryItem)
	ImportLibrary     func(func([]whiteboard.LibraryItem, error))
	ExportLibrary     func([]whiteboard.LibraryItem)
	// AI runs the board's AI nodes; nil hides the AI tools.
	AI *BoardAI
}

// Board owns an editing session. Persisted content is separate from viewport,
// hover, selection and IME preedit, so navigation cannot dirty user files.
type Board struct {
	annotationBase                             string
	Scene                                      *whiteboard.Scene
	History                                    whiteboard.History
	Selected                                   map[string]bool
	Tool                                       BoardTool
	ReadOnly, RequestFocus, ToolLocked         bool
	Zoom, PanX, PanY                           float64
	ShowGrid, Snap, ArrowBinding, MidpointSnap bool
	style                                      *whiteboard.Element
	compactStyleOpen                           bool
	lastRect                                   ui.Rect
	fitted                                     bool
	revision                                   uint64
	services                                   ui.Services
	options                                    BoardOptions
	mode                                       string
	multiLine                                  bool
	moveApplied                                whiteboard.Point
	start, last                                whiteboard.Point
	before                                     *whiteboard.Scene
	creating                                   string
	lastCreated                                string
	handle                                     int
	rotateStart                                float64
	selectionBox                               whiteboard.Box
	lasso                                      []whiteboard.Point
	spaceDown                                  bool
	gestureCursor                              ui.Cursor
	gestureChanged                             bool
	gestureUntil                               time.Time
	hover                                      whiteboard.Point
	editing                                    *boardTextSession
	textFocus                                  bool
	linearEdit                                 string
	linearPoint                                int
	isolatedGroup                              string
	groupRename                                string
	groups                                     boardGroupCache
	styleClipboard                             *whiteboard.Element
	paths                                      map[string]boardPathCache
	bounds                                     map[string]boardBoundsCache
	viewCache                                  boardViewCache
	texts                                      map[string]boardTextCache
	images                                     map[string]*ui.Bitmap
	imageErrors                                map[string]error
	imagePending                               map[string]bool
	imageWanted                                map[string]bool
	imageLoads                                 int
	err                                        error
	panel                                      string
	propertyInput                              string
	mermaidInput                               string
	outline                                    string
	search                                     boardSearchState
	libraryQuery                               string
	libraryName                                string
	libraryPreviews                            map[string]*Board
	cropID                                     string
	cropValues                                 [4]string
	cropper                                    *Editor
	ai                                         boardAIState
	// aiPainting is set while the interactive view paints, where node
	// cards get live widgets; exports draw the widgets' values instead.
	aiPainting bool
}

func NewBoard(s *whiteboard.Scene) *Board {
	if s == nil {
		s = whiteboard.NewScene()
	}
	style := whiteboard.NewElement(whiteboard.KindRectangle, 0, 0, 0, 0)
	style.DimArrow = true
	style.EndArrowhead = whiteboard.ArrowTriangle
	return &Board{Scene: s, History: whiteboard.NewHistory(), Selected: map[string]bool{}, Zoom: 1, ArrowBinding: true, MidpointSnap: true, style: style, paths: map[string]boardPathCache{}, texts: map[string]boardTextCache{}, images: map[string]*ui.Bitmap{}, imageErrors: map[string]error{}, imagePending: map[string]bool{}}
}
func (b *Board) Revision() uint64 { return b.revision }

// Snapshot excludes an unfinished pointer gesture. IME preedit is already
// outside Scene; only committed text and completed gestures reach persistence.
func (b *Board) Snapshot() *whiteboard.Scene {
	if b.before != nil && b.mode != "" {
		return b.before.Clone()
	}
	return b.Scene.Clone()
}

func (b *Board) FinishEditing() {
	b.finishText(b.options)
	if b.multiLine {
		b.finishLine(b.options)
	}
}

// ReplaceScene is a single undoable replacement used by version restoration.
func (b *Board) ReplaceScene(s *whiteboard.Scene) {
	b.edit(b.options, func() {
		b.Scene = s.Clone()
		b.Selected = map[string]bool{}
		clear(b.paths)
		clear(b.texts)
		clear(b.images)
	})
}
func (b *Board) SelectedIDs() []string {
	if len(b.Selected) == 0 {
		return nil
	}
	var ids []string
	for _, e := range b.Scene.Elements {
		if b.Selected[e.ID] && !e.IsDeleted {
			ids = append(ids, e.ID)
		}
	}
	return ids
}
func (b *Board) selectIDs(ids []string) {
	b.Selected = map[string]bool{}
	for _, id := range ids {
		b.Selected[id] = true
	}
}
func (b *Board) changed(o BoardOptions) {
	b.revision++
	if o.OnChange != nil {
		o.OnChange()
	}
}
func (b *Board) edit(o BoardOptions, fn func()) {
	if b.ReadOnly {
		return
	}
	b.flushAIEdit()
	b.History.Begin(b.Scene)
	fn()
	if b.History.Commit(b.Scene) {
		b.changed(o)
	}
}
func (b *Board) Undo() bool {
	if b.ReadOnly {
		return false
	}
	b.flushAIEdit()
	s, ok := b.History.Undo(b.Scene)
	if ok {
		b.Scene = s
		b.cleanSelection()
		b.revision++
	}
	return ok
}
func (b *Board) Redo() bool {
	if b.ReadOnly {
		return false
	}
	b.flushAIEdit()
	s, ok := b.History.Redo(b.Scene)
	if ok {
		b.Scene = s
		b.cleanSelection()
		b.revision++
	}
	return ok
}
func (b *Board) cleanSelection() {
	for id := range b.Selected {
		e := b.Scene.ElementByID(id)
		if e == nil || e.IsDeleted {
			delete(b.Selected, id)
		}
	}
}
func (b *Board) RevealElement(id string) {
	if e := b.Scene.ElementByID(id); e != nil {
		b.selectIDs([]string{id})
		c := whiteboard.ElementBounds(e).Center()
		b.PanX = float64(b.lastRect.W)/2 - c[0]*b.Zoom
		b.PanY = float64(b.lastRect.H)/2 - c[1]*b.Zoom
		b.fitted = true
	}
}
func (b *Board) scenePoint(x, y float32) whiteboard.Point {
	return whiteboard.Point{(float64(x) - b.PanX) / b.Zoom, (float64(y) - b.PanY) / b.Zoom}
}
func (b *Board) screenPoint(p whiteboard.Point) (float32, float32) {
	return float32(p[0]*b.Zoom+b.PanX) + b.lastRect.X, float32(p[1]*b.Zoom+b.PanY) + b.lastRect.Y
}
func (b *Board) fit(selection bool) {
	bounds := whiteboard.SceneBounds(b.Scene)
	if selection {
		bounds = whiteboard.SelectionBounds(b.Scene, b.SelectedIDs())
	}
	if bounds.Empty() {
		b.Zoom = 1
		b.PanX, b.PanY = 0, 0
		return
	}
	w, h := float64(b.lastRect.W), float64(b.lastRect.H)
	if w <= 0 || h <= 0 {
		return
	}
	b.Zoom = math.Max(.1, math.Min(2, math.Min((w-120)/math.Max(1, bounds.W), (h-120)/math.Max(1, bounds.H))))
	c := bounds.Center()
	b.PanX = w/2 - c[0]*b.Zoom
	b.PanY = h/2 - c[1]*b.Zoom
	b.fitted = true
}
func (b *Board) zoomAt(x, y float32, factor float64) {
	if factor != 1 {
		b.gestureCursor = ui.CursorZoomIn
		if factor < 1 {
			b.gestureCursor = ui.CursorZoomOut
		}
		b.gestureChanged = true
	}
	p := b.scenePoint(x, y)
	b.Zoom = math.Max(.05, math.Min(30, b.Zoom*factor))
	b.PanX = float64(x) - p[0]*b.Zoom
	b.PanY = float64(y) - p[1]*b.Zoom
	b.fitted = true
}
func (b *Board) newElement(kind whiteboard.ElementKind, p whiteboard.Point, w, h float64) *whiteboard.Element {
	e := whiteboard.NewElement(kind, p[0], p[1], w, h)
	copyBoardStyle(e, b.style)
	if kind == whiteboard.KindNote {
		e.FillColor = "#ffdf6b"
		e.FillStyle = whiteboard.FillSolid
		e.BaseHeight = 250
	}
	if kind == whiteboard.KindText {
		e.FontSize = b.style.FontSize
		if e.FontSize <= 0 {
			e.FontSize = 20
		}
		e.FontFamily = b.style.FontFamily
		if e.FontFamily == 0 {
			e.FontFamily = 5
		}
		e.TextAlign = "left"
		e.VerticalAlign = "top"
		e.AutoResize = true
		e.LineHeight = whiteboard.FontLineHeight(e.FontFamily)
	}
	return e
}
func copyBoardStyle(e, from *whiteboard.Element) {
	e.StrokeColor = from.StrokeColor
	e.FillColor = from.FillColor
	e.FillStyle = from.FillStyle
	e.StrokeWidth = from.StrokeWidth
	e.StrokeStyle = from.StrokeStyle
	e.Roughness = from.Roughness
	e.Opacity = from.Opacity
	if e.IsLinear() {
		e.DimArrow = from.DimArrow
		if e.Kind == whiteboard.KindArrow {
			e.StartArrowhead, e.EndArrowhead = from.StartArrowhead, from.EndArrowhead
		}
		e.Elbowed, e.Roundness = from.Elbowed, from.Roundness
	}
	if e.Kind == whiteboard.KindText && from.FontSize > 0 {
		e.FontSize = from.FontSize
		e.FontFamily = from.FontFamily
	}
}

func (b *Board) InsertImage(data []byte, at whiteboard.Point) error {
	return b.insertImage(data, at, !b.options.ImageAnnotation)
}

// User image insertion creates an input node in the same undo transaction.
// Explicit ordinary output and annotation insertion keep their image form.
func (b *Board) insertImage(data []byte, at whiteboard.Point, node bool) error {
	if b.ReadOnly {
		return fmt.Errorf("whiteboard is read-only")
	}
	if len(data) > 32<<20 {
		return fmt.Errorf("image exceeds 32 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100_000_000 {
		return fmt.Errorf("image dimensions exceed limit")
	}
	mime := "image/" + format
	if format == "jpg" {
		mime = "image/jpeg"
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	w, h := float64(cfg.Width), float64(cfg.Height)
	scale := math.Min(1, 600/math.Max(w, h))
	w *= scale
	h *= scale
	if node && b.lastRect.W > 0 && b.lastRect.H > 0 {
		at = b.viewCenter()
	}
	b.edit(b.options, func() {
		e := whiteboard.NewElement(whiteboard.KindImage, at[0]-w/2, at[1]-h/2, w, h)
		e.FileID = id
		e.Status = "saved"
		if node {
			b.makeImageNode(e)
		}
		b.Scene.Files[id] = &whiteboard.FileData{ID: id, MIMEType: mime, DataURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}
		b.Scene.Elements = append(b.Scene.Elements, e)
		b.selectIDs([]string{e.ID})
	})
	return nil
}
func (b *Board) SetImageCrop(id string, crop whiteboard.ImageCrop) error {
	e := b.Scene.ElementByID(id)
	if e == nil || e.Kind != whiteboard.KindImage {
		return fmt.Errorf("select an image")
	}
	if crop.X < 0 || crop.Y < 0 || crop.Width <= 0 || crop.Height <= 0 || crop.X+crop.Width > crop.NaturalWidth || crop.Y+crop.Height > crop.NaturalHeight {
		return fmt.Errorf("crop is outside image")
	}
	b.edit(b.options, func() { e.Crop = &crop; whiteboard.Touch(e) })
	return nil
}

func (b *Board) RunAction(action string, o BoardOptions) {
	if o.ImageAnnotation {
		switch action {
		case "clear", "clear-confirm", "lock", "unlock-all", "insert-image", "paste", "frame", "remove-frame", "library", "background":
			return
		}
		delete(b.Selected, b.annotationBase)
	}
	ids := b.SelectedIDs()
	switch action {
	case "select-all":
		b.Selected = map[string]bool{}
		for _, e := range b.Scene.Elements {
			if !e.IsDeleted {
				b.Selected[e.ID] = true
			}
		}
		return
	case "deselect":
		b.Selected = map[string]bool{}
		return
	case "zoom-in":
		b.zoomAt(b.lastRect.W/2, b.lastRect.H/2, 1.2)
		return
	case "zoom-out":
		b.zoomAt(b.lastRect.W/2, b.lastRect.H/2, 1/1.2)
		return
	case "reset-zoom":
		b.zoomAt(b.lastRect.W/2, b.lastRect.H/2, 1/b.Zoom)
		return
	case "fit":
		b.fit(false)
		return
	case "fit-selection":
		b.fit(true)
		return
	case "snap":
		b.Snap = !b.Snap
		return
	case "tool-lock":
		b.ToolLocked = !b.ToolLocked
		return
	case "arrow-binding":
		b.ArrowBinding = !b.ArrowBinding
		return
	case "midpoint-snap":
		b.MidpointSnap = !b.MidpointSnap
		return
	case "help":
		b.panel = "help"
		return
	case "search":
		b.panel = "search"
		b.search.focus = true
		return
	case "copy":
		b.copyBoard(false)
		return
	case "copy-svg":
		b.copySVG()
		return
	case "copy-png":
		b.copyPNG()
		return
	case "copy-text":
		var text []string
		for _, id := range b.Scene.ExpandSelection(ids, false) {
			e := b.Scene.ElementByID(id)
			if e.Kind == whiteboard.KindText {
				text = append(text, e.OriginalText)
			}
		}
		b.services.WriteClipboard(strings.Join(text, "\n"))
		return
	case "copy-style":
		if len(ids) > 0 {
			b.styleClipboard = b.Scene.ElementByID(ids[0]).Clone()
		}
		return
	case "undo":
		if b.Undo() && o.OnChange != nil {
			o.OnChange()
		}
		return
	case "redo":
		if b.Redo() && o.OnChange != nil {
			o.OnChange()
		}
		return
	}
	if b.ReadOnly {
		return
	}
	if action == "rename-group" {
		b.beginGroupRename(b.selectedGroup())
		return
	}
	if action == "edit-group" {
		b.editGroup(b.selectedGroup())
		return
	}
	if action == "exit-group" {
		b.exitGroup()
		return
	}
	if action == "paste" {
		b.pasteBoard(o)
		return
	}
	if action == "cut" {
		b.copyBoard(false)
		action = "delete"
	}
	if action == "insert-image" {
		if o.PickImage != nil {
			o.PickImage()
		}
		return
	}
	if action == "clear" {
		b.panel = "clear"
		return
	}
	if action == "crop" {
		if len(ids) == 1 {
			b.beginCrop(ids[0])
		}
		return
	}
	if action == "edit-line" {
		if len(ids) == 1 && b.Scene.ElementByID(ids[0]).IsLinear() {
			b.linearEdit = ids[0]
		}
		return
	}
	b.edit(o, func() {
		switch action {
		case "delete":
			b.Scene.Delete(ids)
			b.cleanSelection()
		case "clear-confirm":
			b.Scene.Elements = nil
			b.Scene.Files = map[string]*whiteboard.FileData{}
			b.Selected = map[string]bool{}
			b.panel = ""
		case "duplicate":
			clip, err := b.Scene.Clipboard(ids)
			if err != nil {
				b.err = err
				return
			}
			at := whiteboard.SelectionBounds(b.Scene, ids).Center()
			next, err := b.Scene.Paste(clip, whiteboard.Add(at, whiteboard.Point{20, 20}))
			if err != nil {
				b.err = err
				return
			}
			b.selectIDs(next)
		case "group":
			b.Scene.Group(ids)
		case "ungroup":
			b.Scene.Ungroup(ids)
		case "front", "back", "forward", "backward":
			b.Scene.Reorder(ids, action)
		case "flip-horizontal":
			b.Scene.Flip(ids, true)
		case "flip-vertical":
			b.Scene.Flip(ids, false)
		case "lock":
			for _, id := range ids {
				e := b.Scene.ElementByID(id)
				e.Locked = !e.Locked
				whiteboard.Touch(e)
			}
		case "unlock-all":
			for _, e := range b.Scene.Elements {
				if e.Locked {
					e.Locked = false
					whiteboard.Touch(e)
				}
			}
		case "paste-style":
			if b.styleClipboard != nil {
				for _, id := range ids {
					e := b.Scene.ElementByID(id)
					copyBoardStyle(e, b.styleClipboard)
					whiteboard.Touch(e)
				}
			}
		case "frame":
			f := b.Scene.FrameFromSelection(ids)
			b.selectIDs([]string{f.ID})
		case "remove-frame":
			b.Scene.RemoveFromFrame(ids)
		case "select-frame":
			if len(ids) == 1 {
				b.selectIDs(b.Scene.ExpandSelection(ids, false))
			}
		case "grid":
			b.Scene.AppState.GridModeEnabled = !b.Scene.AppState.GridModeEnabled
			b.ShowGrid = b.Scene.AppState.GridModeEnabled
		case "unbind-text":
			for _, id := range ids {
				e := b.Scene.ElementByID(id)
				if e.Kind == whiteboard.KindText {
					b.Scene.UnbindText(id)
				} else if label := e.BoundTextID(); label != "" {
					b.Scene.UnbindText(label)
				}
			}
		case "bind-text":
			if len(ids) == 2 {
				a, z := b.Scene.ElementByID(ids[0]), b.Scene.ElementByID(ids[1])
				if a.Kind != whiteboard.KindText {
					a, z = z, a
				}
				b.err = b.Scene.BindText(a.ID, z.ID, boardMeasure)
			}
		case "wrap-text":
			for _, id := range ids {
				e := b.Scene.ElementByID(id)
				if e.Kind != whiteboard.KindText {
					continue
				}
				r := whiteboard.NewElement(whiteboard.KindRectangle, e.X-10, e.Y-10, e.W+20, e.H+20)
				b.Scene.Elements = append(b.Scene.Elements, r)
				b.Scene.Reorder([]string{r.ID}, "back")
				b.err = b.Scene.BindText(id, r.ID, boardMeasure)
			}
		case "auto-text":
			for _, id := range ids {
				e := b.Scene.ElementByID(id)
				if e.Kind == whiteboard.KindText {
					e.AutoResize = !e.AutoResize
					b.Scene.LayoutText(id, boardMeasure)
					whiteboard.Touch(e)
				}
			}
		default:
			if strings.HasPrefix(action, "align-") {
				p := strings.Split(action, "-")
				axis := 0
				if p[1] == "y" {
					axis = 1
				}
				b.Scene.Align(ids, axis, p[2])
			} else if action == "distribute-x" {
				b.Scene.Distribute(ids, 0)
			} else if action == "distribute-y" {
				b.Scene.Distribute(ids, 1)
			}
		}
	})
}
func (b *Board) ChangeProperty(name string, value any, o BoardOptions) {
	apply := func(e *whiteboard.Element) {
		switch name {
		case "stroke":
			e.StrokeColor = value.(string)
		case "fill":
			e.FillColor = value.(string)
		case "fill-style":
			e.FillStyle = whiteboard.FillStyle(value.(string))
		case "width":
			e.StrokeWidth = value.(float64)
		case "stroke-style":
			e.StrokeStyle = whiteboard.StrokeStyle(value.(string))
		case "roughness":
			e.Roughness = value.(float64)
		case "opacity":
			e.Opacity = value.(float64)
		case "font-size":
			e.FontSize = value.(float64)
		case "font":
			e.FontFamily = value.(int)
		case "text-align":
			e.TextAlign = value.(string)
		case "vertical-align":
			e.VerticalAlign = value.(string)
		case "round":
			if value.(bool) {
				e.Roundness = &whiteboard.Roundness{Type: 3}
			} else {
				e.Roundness = nil
			}
		case "start-arrow":
			e.StartArrowhead = whiteboard.Arrowhead(value.(string))
			e.DimArrow = true
		case "end-arrow":
			e.EndArrowhead = whiteboard.Arrowhead(value.(string))
			e.DimArrow = true
		case "dim0-arrow":
			e.DimArrow = value.(bool)
		case "routing":
			v := value.(string)
			e.DimArrow = true
			e.Elbowed = v == "elbow"
			if v == "curved" {
				e.Roundness = &whiteboard.Roundness{Type: 2}
			} else {
				e.Roundness = nil
			}
			if e.Elbowed && len(e.Points) > 1 {
				start := whiteboard.Point{e.X, e.Y}
				end := whiteboard.Add(start, e.Points[len(e.Points)-1])
				e.Points = whiteboard.ElbowRoute(start, end, e.StartBinding, e.EndBinding)
			}
		case "link":
			e.Link = value.(string)
		}
		whiteboard.Touch(e)
	}
	apply(b.style)
	b.edit(o, func() {
		ids := b.SelectedIDs()
		if name == "font" || name == "font-size" || name == "text-align" || name == "vertical-align" {
			ids = b.Scene.ExpandSelection(ids, false)
		}
		for _, id := range ids {
			e := b.Scene.ElementByID(id)
			if (name == "font" || name == "font-size" || name == "text-align" || name == "vertical-align") && e.Kind != whiteboard.KindText {
				continue
			}
			if !e.Locked {
				apply(e)
				if e.Kind == whiteboard.KindText {
					b.Scene.LayoutText(id, boardMeasure)
				}
			}
		}
	})
}

func (b *Board) selectedScene() *whiteboard.Scene {
	ids := b.SelectedIDs()
	if len(ids) == 0 {
		return b.Scene.Clone()
	}
	clip, _ := b.Scene.Clipboard(ids)
	var raw struct {
		Elements []*whiteboard.Element           `json:"elements"`
		Files    map[string]*whiteboard.FileData `json:"files"`
	}
	_ = json.Unmarshal([]byte(clip), &raw)
	s := whiteboard.NewScene()
	s.Elements = raw.Elements
	s.Files = raw.Files
	s.AppState = b.Scene.AppState
	return s
}
func (b *Board) Close() {
	b.paths = nil
	b.bounds = nil
	b.viewCache = boardViewCache{}
	b.texts = nil
	b.images = nil
	b.imageErrors = nil
	b.imagePending = nil
	b.imageWanted = nil
	b.before = nil
	b.editing = nil
	b.StopAI()
	for _, v := range b.ai.videos {
		v.Close()
	}
	b.ai = boardAIState{}
}
func (b *Board) resetFrom(before *whiteboard.Scene) { b.Scene = before.Clone() }
func boardDeleteElement(s *whiteboard.Scene, id string) {
	s.Delete([]string{id})
	s.Elements = slices.DeleteFunc(s.Elements, func(e *whiteboard.Element) bool { return e.ID == id })
	_ = s.ElementByID(id)
}
