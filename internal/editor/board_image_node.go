package editor

import (
	"bytes"
	"encoding/json"
	"image"
	"math"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
)

const imageGeometryKey = "freefolioImageGeometry"

type imageGeometry struct{ Width, Height, Angle float64 }

// Keep original picture geometry so conversion back reuses the same attachment.
func (b *Board) makeImageNode(e *whiteboard.Element) {
	data := map[string]json.RawMessage{}
	if len(e.CustomData) > 0 {
		if err := json.Unmarshal(e.CustomData, &data); err != nil {
			b.err = err
			return
		}
	}
	raw, _ := json.Marshal(imageGeometry{e.W, e.H, e.Angle})
	data[imageGeometryKey] = raw
	e.CustomData, _ = json.Marshal(data)
	n := &whiteboard.AINode{Role: whiteboard.AIImageInput}
	if err := whiteboard.SetAINode(e, n); err != nil {
		b.err = err
		return
	}
	cx, cy := e.X+e.W/2, e.Y+e.H/2
	w, h := whiteboard.AINodeSize(n.Role)
	e.W, e.H = math.Max(w, e.W), math.Max(h, e.H)
	e.X, e.Y, e.Angle = cx-e.W/2, cy-e.H/2, 0
}
func (b *Board) canConvertNodeToImage(e *whiteboard.Element, o BoardOptions) bool {
	if e == nil || e.IsDeleted || e.Locked || b.ReadOnly || o.ReadOnly || b.AIRunning() {
		return false
	}
	n := b.aiNode(e)
	if n == nil {
		return false
	}
	return n.Role == whiteboard.AIImageInput && e.FileID != "" || n.Output() != nil && n.Output().Kind == "image" && o.AI != nil && o.AI.ReadFile != nil
}
func (b *Board) convertNodeToImage(id string, o BoardOptions) {
	e := b.Scene.ElementByID(id)
	if !b.canConvertNodeToImage(e, o) {
		return
	}
	n := b.aiNode(e)
	data := map[string]json.RawMessage{}
	if err := json.Unmarshal(e.CustomData, &data); err != nil {
		b.err = err
		return
	}
	geometry := imageGeometry{}
	if raw := data[imageGeometryKey]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &geometry); err != nil {
			b.err = err
			return
		}
	}
	fileID := e.FileID
	var file *whiteboard.FileData
	if n.Role != whiteboard.AIImageInput || fileID == "" {
		out := n.Output()
		if out == nil {
			return
		}
		rel := out.Path
		if selected := b.DisplayedAIImage(id); selected != "" {
			rel = selected
		}
		pixels, err := o.AI.ReadFile(rel)
		if err != nil {
			b.err = err
			return
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(pixels))
		if err != nil {
			b.err = err
			return
		}
		fileID = digest([]byte(rel))
		file = &whiteboard.FileData{ID: fileID, Path: rel, MIMEType: "image/" + format}
		geometry.Width, geometry.Height = float64(cfg.Width), float64(cfg.Height)
		scale := math.Min(1, 600/math.Max(geometry.Width, geometry.Height))
		geometry.Width *= scale
		geometry.Height *= scale
	}
	if geometry.Width <= 0 || geometry.Height <= 0 {
		geometry.Width, geometry.Height = e.W, e.H
	}
	delete(data, whiteboard.AINamespace)
	delete(data, imageGeometryKey)
	raw, err := json.Marshal(data)
	if err != nil {
		b.err = err
		return
	}
	b.edit(o, func() {
		cx, cy := e.X+e.W/2, e.Y+e.H/2
		e.Kind = whiteboard.KindImage
		e.FileID = fileID
		e.Status = "saved"
		e.CustomData = raw
		e.W, e.H, e.Angle = geometry.Width, geometry.Height, geometry.Angle
		e.X, e.Y = cx-e.W/2, cy-e.H/2
		if file != nil {
			b.Scene.Files[fileID] = file
			e.Crop = nil
			e.Scale = whiteboard.Point{1, 1}
		}
		whiteboard.Touch(e)
		b.selectIDs([]string{id})
	})
}
