// Clipboard exchange follows Quiet.md board/clipboard.rs and Excalidraw's
// excalidraw/clipboard contract (MIT). Paste creates a new identity graph.
package whiteboard

import (
	"encoding/json"
	"fmt"
	"math"
)

type clipboardData struct {
	Type     string               `json:"type"`
	Elements []*Element           `json:"elements"`
	Files    map[string]*FileData `json:"files"`
}

func (s *Scene) Clipboard(ids []string) (string, error) {
	chosen := map[string]bool{}
	for _, id := range ids {
		chosen[id] = true
	}
	// Bound labels belong to their container even when only the shape is picked.
	for _, e := range s.Elements {
		if chosen[e.ID] && e.BoundTextID() != "" {
			chosen[e.BoundTextID()] = true
		}
	}
	c := clipboardData{Type: "excalidraw/clipboard", Files: map[string]*FileData{}}
	for _, e := range s.Elements {
		if !chosen[e.ID] || e.IsDeleted {
			continue
		}
		c.Elements = append(c.Elements, e.Clone())
		if f := s.Files[e.FileID]; f != nil {
			c.Files[e.FileID] = f.Clone()
		}
	}
	b, err := json.Marshal(c)
	return string(b), err
}

func (s *Scene) Paste(text string, at Point) ([]string, error) {
	if len(text) > MaxSceneBytes {
		return nil, fmt.Errorf("whiteboard: clipboard exceeds size limit")
	}
	var c clipboardData
	if err := json.Unmarshal([]byte(text), &c); err != nil {
		return nil, err
	}
	if c.Type != "excalidraw/clipboard" {
		return nil, fmt.Errorf("whiteboard: unsupported clipboard")
	}
	tmp := NewScene()
	tmp.Elements = c.Elements
	tmp.Files = c.Files
	if err := tmp.Validate(); err != nil {
		return nil, err
	}
	if len(c.Elements) == 0 {
		return nil, nil
	}
	ids := map[string]string{}
	groups := map[string]string{}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, e := range c.Elements {
		ids[e.ID] = NewID()
		minX = math.Min(minX, e.X)
		minY = math.Min(minY, e.Y)
		maxX = math.Max(maxX, e.X+e.W)
		maxY = math.Max(maxY, e.Y+e.H)
	}
	dx, dy := at[0]-(minX+maxX)/2, at[1]-(minY+maxY)/2
	fileIDs := map[string]string{}
	for id, f := range c.Files {
		newID := id
		if old := s.Files[id]; old != nil && (old.DataURL != f.DataURL || old.Path != f.Path || old.MIMEType != f.MIMEType) {
			newID = NewID()
		}
		fileIDs[id] = newID
	}
	var out []string
	for _, e := range c.Elements {
		e.ID = ids[e.ID]
		e.X += dx
		e.Y += dy
		e.Index = ""
		e.Version = 1
		e.VersionNonce = NewSeed()
		e.FrameID = ids[e.FrameID]
		e.ContainerID = ids[e.ContainerID]
		for _, b := range []*Binding{e.StartBinding, e.EndBinding} {
			if b != nil {
				b.ElementID = ids[b.ElementID]
			}
		}
		if e.StartBinding != nil && e.StartBinding.ElementID == "" {
			e.StartBinding = nil
		}
		if e.EndBinding != nil && e.EndBinding.ElementID == "" {
			e.EndBinding = nil
		}
		var bound []BoundElement
		for _, b := range e.BoundElements {
			if id := ids[b.ID]; id != "" {
				b.ID = id
				bound = append(bound, b)
			}
		}
		e.BoundElements = bound
		for i, g := range e.GroupIDs {
			if groups[g] == "" {
				groups[g] = NewID()
			}
			e.GroupIDs[i] = groups[g]
		}
		if id := fileIDs[e.FileID]; id != "" {
			e.FileID = id
		}
		out = append(out, e.ID)
	}
	if s.Files == nil {
		s.Files = map[string]*FileData{}
	}
	for id, f := range c.Files {
		f.ID = fileIDs[id]
		s.Files[f.ID] = f
	}
	s.Elements = append(s.Elements, c.Elements...)
	return out, nil
}
