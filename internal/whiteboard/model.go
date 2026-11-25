// Package whiteboard implements the native Excalidraw scene used by FreeFolio.
// The model follows Quiet.md e59c49a's board/types.rs and Excalidraw 3a317b5
// (MIT). UI and filesystem ownership deliberately stay outside this package.
package whiteboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

const CurrentFormatVersion = 2

type Point [2]float64
type ElementKind string
type FillStyle string
type StrokeStyle string
type Arrowhead string

const (
	KindRectangle        ElementKind = "rectangle"
	KindDiamond          ElementKind = "diamond"
	KindEllipse          ElementKind = "ellipse"
	KindArrow            ElementKind = "arrow"
	KindLine             ElementKind = "line"
	KindFreedraw         ElementKind = "freedraw"
	KindText             ElementKind = "text"
	KindNote             ElementKind = "stickynote"
	KindImage            ElementKind = "image"
	KindFrame            ElementKind = "frame"
	KindMagicFrame       ElementKind = "magicframe"
	KindEmbeddable       ElementKind = "embeddable"
	KindIframe           ElementKind = "iframe"
	FillNone             FillStyle   = ""
	FillHachure          FillStyle   = "hachure"
	FillCrossHatch       FillStyle   = "cross-hatch"
	FillSolid            FillStyle   = "solid"
	FillZigzag           FillStyle   = "zigzag"
	StrokeSolid          StrokeStyle = "solid"
	StrokeDashed         StrokeStyle = "dashed"
	StrokeDotted         StrokeStyle = "dotted"
	ArrowNone            Arrowhead   = ""
	ArrowOpen            Arrowhead   = "arrow"
	ArrowBar             Arrowhead   = "bar"
	ArrowCircle          Arrowhead   = "circle"
	ArrowCircleOutline   Arrowhead   = "circle_outline"
	ArrowTriangle        Arrowhead   = "triangle"
	ArrowTriangleOutline Arrowhead   = "triangle_outline"
	ArrowDiamond         Arrowhead   = "diamond"
	ArrowDiamondOutline  Arrowhead   = "diamond_outline"
)

// ExtraFields retain data introduced by other applications and newer versions.
type ExtraFields map[string]json.RawMessage

type Roundness struct {
	Type  int         `json:"type"`
	Value *float64    `json:"value,omitempty"`
	Extra ExtraFields `json:"-"`
}

type Binding struct {
	ElementID  string      `json:"elementId"`
	FixedPoint Point       `json:"fixedPoint"`
	Mode       string      `json:"mode,omitempty"`
	Focus      *float64    `json:"focus,omitempty"`
	Gap        *float64    `json:"gap,omitempty"`
	Extra      ExtraFields `json:"-"`
}

type BoundElement struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type ImageCrop struct {
	Extra         ExtraFields `json:"-"`
	X             float64     `json:"x"`
	Y             float64     `json:"y"`
	Width         float64     `json:"width"`
	Height        float64     `json:"height"`
	NaturalWidth  float64     `json:"naturalWidth"`
	NaturalHeight float64     `json:"naturalHeight"`
}

type FixedSegment struct {
	Start Point `json:"start"`
	End   Point `json:"end"`
	Index int   `json:"index"`
}

type StrokeOptions struct {
	Extra       ExtraFields `json:"-"`
	Variability string      `json:"variability"`
	Streamline  float64     `json:"streamline"`
}

// Element keeps text as a separate bound element, as the exchange format does.
// W/H are local dimensions; all geometry uses float64 until the UI boundary.
type Element struct {
	translations     int64
	shapeNonce       int64
	ID               string          `json:"id"`
	Kind             ElementKind     `json:"type"`
	Shape            string          `json:"freefolioShape,omitempty"`
	DimArrow         bool            `json:"freefolioDim0Arrow,omitempty"`
	X                float64         `json:"x"`
	Y                float64         `json:"y"`
	W                float64         `json:"width"`
	H                float64         `json:"height"`
	Angle            float64         `json:"angle"`
	StrokeColor      string          `json:"strokeColor"`
	FillColor        string          `json:"backgroundColor"`
	FillStyle        FillStyle       `json:"fillStyle"`
	StrokeWidth      float64         `json:"strokeWidth"`
	StrokeStyle      StrokeStyle     `json:"strokeStyle"`
	Roundness        *Roundness      `json:"roundness"`
	Roughness        float64         `json:"roughness"`
	Opacity          float64         `json:"opacity"`
	Seed             int64           `json:"seed"`
	Version          int64           `json:"version"`
	VersionNonce     int64           `json:"versionNonce"`
	Index            string          `json:"index,omitempty"`
	IsDeleted        bool            `json:"isDeleted"`
	GroupIDs         []string        `json:"groupIds"`
	FrameID          string          `json:"frameId,omitempty"`
	BoundElements    []BoundElement  `json:"boundElements"`
	Updated          int64           `json:"updated"`
	Created          *int64          `json:"created,omitempty"`
	Link             string          `json:"link,omitempty"`
	Locked           bool            `json:"locked"`
	CustomData       json.RawMessage `json:"customData,omitempty"`
	Points           []Point         `json:"points,omitempty"`
	StartBinding     *Binding        `json:"startBinding,omitempty"`
	EndBinding       *Binding        `json:"endBinding,omitempty"`
	StartArrowhead   Arrowhead       `json:"startArrowhead,omitempty"`
	EndArrowhead     Arrowhead       `json:"endArrowhead,omitempty"`
	Elbowed          bool            `json:"elbowed,omitempty"`
	FixedSegments    []FixedSegment  `json:"fixedSegments,omitempty"`
	StartIsSpecial   *bool           `json:"startIsSpecial,omitempty"`
	EndIsSpecial     *bool           `json:"endIsSpecial,omitempty"`
	Polygon          bool            `json:"polygon,omitempty"`
	Pressures        []float64       `json:"pressures,omitempty"`
	SimulatePressure bool            `json:"simulatePressure,omitempty"`
	StrokeOptions    *StrokeOptions  `json:"strokeOptions,omitempty"`
	Text             string          `json:"text,omitempty"`
	OriginalText     string          `json:"originalText,omitempty"`
	FontSize         float64         `json:"fontSize,omitempty"`
	FontFamily       int             `json:"fontFamily,omitempty"`
	BaseFontSize     *float64        `json:"baseFontSize,omitempty"`
	TextAlign        string          `json:"textAlign,omitempty"`
	VerticalAlign    string          `json:"verticalAlign,omitempty"`
	ContainerID      string          `json:"containerId,omitempty"`
	AutoResize       bool            `json:"autoResize,omitempty"`
	LineHeight       float64         `json:"lineHeight,omitempty"`
	LabelPosition    json.RawMessage `json:"labelPosition,omitempty"`
	BaseHeight       float64         `json:"baseHeight,omitempty"`
	Name             string          `json:"name,omitempty"`
	FileID           string          `json:"fileId,omitempty"`
	Status           string          `json:"status,omitempty"`
	Scale            Point           `json:"scale,omitempty"`
	Crop             *ImageCrop      `json:"crop,omitempty"`
	Extra            ExtraFields     `json:"-"`
}

// ShapeRevision excludes pure translations, which do not change local paths.
func (e *Element) ShapeRevision() (int64, int64) {
	nonce := e.VersionNonce
	if e.translations > 0 {
		nonce = e.shapeNonce
	}
	return e.Version - e.translations, nonce
}

type FileData struct {
	ID            string      `json:"id"`
	MIMEType      string      `json:"mimeType"`
	DataURL       string      `json:"dataURL,omitempty"`
	Path          string      `json:"path,omitempty"`
	Created       int64       `json:"created"`
	LastRetrieved *int64      `json:"lastRetrieved,omitempty"`
	Version       *int64      `json:"version,omitempty"`
	Extra         ExtraFields `json:"-"`
}

type AppState struct {
	ViewBackgroundColor string      `json:"viewBackgroundColor"`
	GridSize            float64     `json:"gridSize"`
	GridStep            float64     `json:"gridStep"`
	GridModeEnabled     bool        `json:"gridModeEnabled"`
	Extra               ExtraFields `json:"-"`
}

type Scene struct {
	byID         map[string]*Element
	indexedCount int
	Type         string               `json:"type"`
	Version      int                  `json:"version"`
	Source       string               `json:"source"`
	Elements     []*Element           `json:"elements"`
	AppState     AppState             `json:"appState"`
	Files        map[string]*FileData `json:"files"`
	Extra        ExtraFields          `json:"-"`
}

// NewID returns an opaque identifier; callers never use names as identities.
func NewID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func NewSeed() int64 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return int64(uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3])) & 0x7fffffff
}

func NewScene() *Scene {
	return &Scene{Type: "excalidraw", Version: 2, Source: "https://github.com/OUBIGFA/FreeFolio", Elements: []*Element{}, Files: map[string]*FileData{}, AppState: AppState{ViewBackgroundColor: "#ffffff", GridSize: 20, GridStep: 5}}
}

func NewElement(kind ElementKind, x, y, w, h float64) *Element {
	e := &Element{ID: NewID(), Kind: kind, X: x, Y: y, W: w, H: h, StrokeColor: "#1e1e1e", FillColor: "transparent", FillStyle: FillHachure, StrokeWidth: 2, StrokeStyle: StrokeSolid, Roughness: 1, Opacity: 100, Seed: NewSeed(), Version: 1, VersionNonce: NewSeed(), GroupIDs: []string{}, Scale: Point{1, 1}}
	switch kind {
	case KindRectangle, KindImage, KindEmbeddable, KindIframe:
		e.Roundness = &Roundness{Type: 3}
	case KindDiamond, KindArrow, KindLine, KindNote:
		e.Roundness = &Roundness{Type: 2}
	}
	switch kind {
	case KindText:
		e.FontSize = 20
		e.FontFamily = 5
		e.TextAlign = "left"
		e.VerticalAlign = "top"
		e.AutoResize = true
		e.LineHeight = 1.25
	case KindNote:
		e.FillColor = "#ffdf6b"
		e.FillStyle = FillSolid
		e.BaseHeight = 250
	case KindArrow, KindLine:
		e.Points = []Point{{0, 0}, {w, h}}
		if kind == KindArrow {
			e.EndArrowhead = ArrowOpen
		}
	case KindFreedraw:
		e.Points = []Point{{0, 0}}
		e.SimulatePressure = true
		e.StrokeOptions = &StrokeOptions{Variability: "variable", Streamline: .5}
	case KindFrame, KindMagicFrame:
		e.Roughness = 0
		e.FillStyle = FillSolid
	}
	return e
}

func (e *Element) IsLinear() bool { return e.Kind == KindArrow || e.Kind == KindLine }
func (e *Element) IsFrame() bool  { return e.Kind == KindFrame || e.Kind == KindMagicFrame }
func (e *Element) IsBindable() bool {
	return !e.Locked && !e.IsDeleted && !e.IsLinear() && e.Kind != KindFreedraw && (e.Kind != KindText || e.ContainerID == "")
}
func (e *Element) BoundTextID() string {
	for _, b := range e.BoundElements {
		if b.Type == "text" {
			return b.ID
		}
	}
	return ""
}
func (s *Scene) ElementByID(id string) *Element {
	if id == "" {
		return nil
	}
	if s.byID == nil || s.indexedCount != len(s.Elements) {
		s.byID = make(map[string]*Element, len(s.Elements))
		for _, e := range s.Elements {
			if e != nil {
				s.byID[e.ID] = e
			}
		}
		s.indexedCount = len(s.Elements)
	}
	return s.byID[id]
}
func (s *Scene) VisibleElements() []*Element {
	var out []*Element
	for _, e := range s.Elements {
		if e != nil && !e.IsDeleted {
			out = append(out, e)
		}
	}
	return out
}

func cloneExtra(extra ExtraFields) ExtraFields {
	if extra == nil {
		return nil
	}
	out := make(ExtraFields, len(extra))
	for k, v := range extra {
		out[k] = slices.Clone(v)
	}
	return out
}
func clonePtr[T any](v *T) *T {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func (b *Binding) Clone() *Binding {
	if b == nil {
		return nil
	}
	c := *b
	c.Extra = cloneExtra(b.Extra)
	c.Focus = clonePtr(b.Focus)
	c.Gap = clonePtr(b.Gap)
	return &c
}
func (e *Element) Clone() *Element {
	if e == nil {
		return nil
	}
	c := *e
	c.Extra = cloneExtra(e.Extra)
	c.CustomData = slices.Clone(e.CustomData)
	c.LabelPosition = slices.Clone(e.LabelPosition)
	c.Points = slices.Clone(e.Points)
	c.Pressures = slices.Clone(e.Pressures)
	c.GroupIDs = slices.Clone(e.GroupIDs)
	c.BoundElements = slices.Clone(e.BoundElements)
	c.FixedSegments = slices.Clone(e.FixedSegments)
	c.StartBinding = e.StartBinding.Clone()
	c.EndBinding = e.EndBinding.Clone()
	c.Roundness = clonePtr(e.Roundness)
	if c.Roundness != nil {
		c.Roundness.Value = clonePtr(c.Roundness.Value)
		c.Roundness.Extra = cloneExtra(c.Roundness.Extra)
	}
	c.Crop = clonePtr(e.Crop)
	if c.Crop != nil {
		c.Crop.Extra = cloneExtra(c.Crop.Extra)
	}
	c.StrokeOptions = clonePtr(e.StrokeOptions)
	if c.StrokeOptions != nil {
		c.StrokeOptions.Extra = cloneExtra(c.StrokeOptions.Extra)
	}
	c.Created = clonePtr(e.Created)
	c.BaseFontSize = clonePtr(e.BaseFontSize)
	c.StartIsSpecial = clonePtr(e.StartIsSpecial)
	c.EndIsSpecial = clonePtr(e.EndIsSpecial)
	return &c
}
func (f *FileData) Clone() *FileData {
	if f == nil {
		return nil
	}
	c := *f
	c.Extra = cloneExtra(f.Extra)
	c.LastRetrieved = clonePtr(f.LastRetrieved)
	c.Version = clonePtr(f.Version)
	return &c
}
func (s *Scene) Clone() *Scene {
	if s == nil {
		return NewScene()
	}
	c := *s
	c.byID = nil
	c.indexedCount = 0
	c.Extra = cloneExtra(s.Extra)
	c.AppState.Extra = cloneExtra(s.AppState.Extra)
	c.Elements = make([]*Element, len(s.Elements))
	for i, e := range s.Elements {
		c.Elements[i] = e.Clone()
	}
	c.Files = make(map[string]*FileData, len(s.Files))
	for k, f := range s.Files {
		c.Files[k] = f.Clone()
	}
	return &c
}

func (s *Scene) PlainText() string {
	var lines []string
	for _, e := range s.Elements {
		if e == nil || e.IsDeleted {
			continue
		}
		if e.Kind == KindText {
			text := e.OriginalText
			if text == "" {
				text = e.Text
			}
			if text != "" {
				lines = append(lines, text)
			}
		}
		if e.IsFrame() && e.Name != "" {
			lines = append(lines, e.Name)
		}
		if e.Link != "" {
			lines = append(lines, e.Link)
		}
	}
	return strings.Join(lines, "\n")
}

func validKind(k ElementKind) bool {
	switch k {
	case KindRectangle, KindDiamond, KindEllipse, KindArrow, KindLine, KindFreedraw, KindText, KindNote, KindImage, KindFrame, KindMagicFrame, KindEmbeddable, KindIframe:
		return true
	}
	return false
}

// Validate rejects unsupported content instead of inventing replacement shapes.
func (s *Scene) Validate() error {
	if s == nil || s.Type != "excalidraw" || s.Version < 1 || s.Version > CurrentFormatVersion {
		return fmt.Errorf("whiteboard: unsupported scene format")
	}
	seen := make(map[string]bool, len(s.Elements))
	for _, e := range s.Elements {
		if e == nil || e.ID == "" || seen[e.ID] {
			return fmt.Errorf("whiteboard: missing or duplicate element ID")
		}
		seen[e.ID] = true
		if !validKind(e.Kind) {
			return fmt.Errorf("whiteboard: unsupported element %q (%s)", e.Kind, e.ID)
		}
		if e.Shape != "" && (e.Kind != KindRectangle || !slices.Contains(DimShapeNames, e.Shape)) {
			return fmt.Errorf("whiteboard: unsupported shape %q (%s)", e.Shape, e.ID)
		}
		values := []float64{e.X, e.Y, e.W, e.H, e.Angle, e.StrokeWidth, e.Roughness, e.Opacity, e.FontSize, e.LineHeight}
		for _, p := range e.Points {
			values = append(values, p[0], p[1])
		}
		for _, v := range values {
			if math.IsInf(v, 0) || math.IsNaN(v) || math.Abs(v) > 1e7 {
				return fmt.Errorf("whiteboard: non-finite geometry (%s)", e.ID)
			}
		}
		if e.Opacity < 0 || e.Opacity > 100 || e.StrokeWidth < 0 || e.Roughness < 0 || e.Roughness > 10 {
			return fmt.Errorf("whiteboard: invalid visual properties (%s)", e.ID)
		}
		if e.Crop != nil && (e.Crop.Width <= 0 || e.Crop.Height <= 0 || e.Crop.NaturalWidth <= 0 || e.Crop.NaturalHeight <= 0) {
			return fmt.Errorf("whiteboard: invalid image crop (%s)", e.ID)
		}
	}
	for key, f := range s.Files {
		if f == nil || f.ID != key {
			return fmt.Errorf("whiteboard: invalid file identifier")
		}
		p := strings.ReplaceAll(f.Path, "\\", "/")
		if strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
			return fmt.Errorf("whiteboard: image path is outside the space")
		}
		for _, part := range strings.Split(p, "/") {
			if part == ".." {
				return fmt.Errorf("whiteboard: image path is outside the space")
			}
		}
	}
	return nil
}
