package whiteboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

const MaxSceneBytes = 128 << 20

// Decode is strict at the file boundary while preserving extensions verbatim.
func Decode(src string) (*Scene, error) {
	if len(src) > MaxSceneBytes {
		return nil, fmt.Errorf("whiteboard: scene exceeds size limit")
	}
	if strings.TrimSpace(src) == "" || strings.TrimSpace(src) == "null" {
		return nil, fmt.Errorf("whiteboard: empty scene")
	}
	if strings.HasPrefix(strings.TrimSpace(src), "<") {
		var err error
		src, err = DecodeSVGPayload(src)
		if err != nil {
			return nil, err
		}
	}
	s := NewScene()
	s.Type = ""
	s.Version = 0
	s.Elements = nil
	if err := json.Unmarshal([]byte(src), s); err != nil {
		return nil, fmt.Errorf("whiteboard: invalid scene: %w", err)
	}
	if s.Elements == nil {
		return nil, fmt.Errorf("whiteboard: missing element array")
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.Files == nil {
		s.Files = map[string]*FileData{}
	}
	s.restoreLegacyBindings()
	return s, nil
}

func (s *Scene) Encode() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	out := *s
	out.Elements = normalizedOrder(s.Elements)
	if out.Elements == nil {
		out.Elements = []*Element{}
	}
	b, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// The alias types prevent recursive JSON methods. Reflection only enumerates
// declared field names, never application values or UI state.
func decodeFields(data []byte, dst any) (ExtraFields, error) {
	var raw ExtraFields
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("expected an object")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return nil, err
	}
	t := reflect.TypeOf(dst).Elem()
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			delete(raw, tag)
		}
	}
	return raw, nil
}
func encodeFields(v any, extra ExtraFields) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return b, nil
	}
	var raw ExtraFields
	if err = json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	for k, v := range extra {
		if _, ok := raw[k]; !ok {
			var compact bytes.Buffer
			if err = json.Compact(&compact, v); err != nil {
				return nil, err
			}
			raw[k] = compact.Bytes()
		}
	}
	return json.Marshal(raw)
}

type elementJSON Element

func (e *Element) UnmarshalJSON(b []byte) error {
	v := elementJSON{Opacity: 100, StrokeColor: "#1e1e1e", FillColor: "transparent", FillStyle: FillHachure, StrokeWidth: 2, StrokeStyle: StrokeSolid, Roughness: 1, Version: 1, Scale: Point{1, 1}, FontSize: 20, FontFamily: 5, LineHeight: 1.25, TextAlign: "left", VerticalAlign: "top", AutoResize: true}
	extra, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*e = Element(v)
	e.Extra = extra
	return nil
}
func (e Element) MarshalJSON() ([]byte, error) {
	b, err := encodeFields(elementJSON(e), e.Extra)
	if err != nil {
		return nil, err
	}
	var raw ExtraFields
	if err = json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if e.Kind == KindText {
		raw["autoResize"], _ = json.Marshal(e.AutoResize)
		raw["text"], _ = json.Marshal(e.Text)
		raw["originalText"], _ = json.Marshal(e.OriginalText)
		if e.ContainerID == "" {
			raw["containerId"] = json.RawMessage("null")
		}
	}
	if e.IsLinear() {
		if e.StartArrowhead == "" {
			raw["startArrowhead"] = json.RawMessage("null")
		}
		if e.EndArrowhead == "" {
			raw["endArrowhead"] = json.RawMessage("null")
		}
		if e.StartBinding == nil {
			raw["startBinding"] = json.RawMessage("null")
		}
		if e.EndBinding == nil {
			raw["endBinding"] = json.RawMessage("null")
		}
	}
	if e.Kind == KindFreedraw {
		raw["simulatePressure"], _ = json.Marshal(e.SimulatePressure)
	}
	if e.Kind != KindImage {
		delete(raw, "scale")
	}
	if e.Kind != KindText {
		for _, k := range []string{"fontFamily", "fontSize", "autoResize", "lineHeight", "textAlign", "verticalAlign"} {
			delete(raw, k)
		}
	}
	return json.Marshal(raw)
}

type sceneJSON Scene

func (s *Scene) UnmarshalJSON(b []byte) error {
	v := sceneJSON(*s)
	x, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*s = Scene(v)
	s.Extra = x
	return nil
}
func (s Scene) MarshalJSON() ([]byte, error) { return encodeFields(sceneJSON(s), s.Extra) }

type fileJSON FileData

func (f *FileData) UnmarshalJSON(b []byte) error {
	var v fileJSON
	x, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*f = FileData(v)
	f.Extra = x
	return nil
}
func (f FileData) MarshalJSON() ([]byte, error) { return encodeFields(fileJSON(f), f.Extra) }

type appJSON AppState

func (a *AppState) UnmarshalJSON(b []byte) error {
	v := appJSON(*a)
	x, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*a = AppState(v)
	a.Extra = x
	return nil
}
func (a AppState) MarshalJSON() ([]byte, error) { return encodeFields(appJSON(a), a.Extra) }

type bindingJSON Binding

func (a *Binding) UnmarshalJSON(b []byte) error {
	var v bindingJSON
	x, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*a = Binding(v)
	a.Extra = x
	return nil
}
func (a Binding) MarshalJSON() ([]byte, error) { return encodeFields(bindingJSON(a), a.Extra) }

type roundnessJSON Roundness

func (r *Roundness) UnmarshalJSON(b []byte) error {
	var v roundnessJSON
	x, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*r = Roundness(v)
	r.Extra = x
	return nil
}
func (r Roundness) MarshalJSON() ([]byte, error) { return encodeFields(roundnessJSON(r), r.Extra) }

type cropJSON ImageCrop

func (r *ImageCrop) UnmarshalJSON(b []byte) error {
	var v cropJSON
	x, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*r = ImageCrop(v)
	r.Extra = x
	return nil
}
func (r ImageCrop) MarshalJSON() ([]byte, error) { return encodeFields(cropJSON(r), r.Extra) }

type strokeJSON StrokeOptions

func (r *StrokeOptions) UnmarshalJSON(b []byte) error {
	var v strokeJSON
	x, err := decodeFields(b, &v)
	if err != nil {
		return err
	}
	*r = StrokeOptions(v)
	r.Extra = x
	return nil
}
func (r StrokeOptions) MarshalJSON() ([]byte, error) { return encodeFields(strokeJSON(r), r.Extra) }

func (s *Scene) restoreLegacyBindings() {
	for _, e := range s.Elements {
		if !e.IsLinear() || len(e.Points) < 2 {
			continue
		}
		for i, b := range []*Binding{e.StartBinding, e.EndBinding} {
			if b == nil || b.Mode != "" {
				continue
			}
			target := s.ElementByID(b.ElementID)
			if target == nil {
				continue
			}
			p := e.Points[0]
			if i == 1 {
				p = e.Points[len(e.Points)-1]
			}
			p = Add(p, Point{e.X, e.Y})
			p = RotatePoint(p, Point{target.X + target.W/2, target.Y + target.H/2}, -target.Angle)
			if target.W > 0 {
				b.FixedPoint[0] = (p[0] - target.X) / target.W
			}
			if target.H > 0 {
				b.FixedPoint[1] = (p[1] - target.Y) / target.H
			}
			b.Mode = "orbit"
		}
	}
}
