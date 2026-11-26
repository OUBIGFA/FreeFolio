package editor

import (
	"bytes"
	"fmt"
	"image"

	"github.com/OUBIGFA/FreeFolio/internal/whiteboard"
	"github.com/egoist/mygo/ui"
)

const boardImageLoadLimit = 2

// Include the dense scene's padded render target so panning into its retained
// pixels never shows an image that was omitted from preparation. Exports and
// image annotation snapshots have no viewport and load all live resources.
func (b *Board) mediaVisible(e *whiteboard.Element) bool {
	if e == nil || e.IsDeleted {
		return false
	}
	if b.lastRect.W <= 0 || b.lastRect.H <= 0 {
		return true
	}
	view := whiteboard.Box{X: -b.PanX / b.Zoom, Y: -b.PanY / b.Zoom, W: float64(b.lastRect.W) / b.Zoom, H: float64(b.lastRect.H) / b.Zoom}
	return view.Expand(240 / b.Zoom).Intersects(b.elementBounds(e))
}

func decodeBoardBitmap(data []byte) (*ui.Bitmap, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > 100_000_000 {
		return nil, fmt.Errorf("image dimensions exceed limit")
	}
	return ui.DecodeBitmap(data)
}

// Share two decode slots between imported pictures and generated previews.
// Defer unscheduled work to the frame requested by the next completion rather
// than spawning a goroutine for every image in a large scene.
func (b *Board) loadBitmap(o BoardOptions, load func() (*ui.Bitmap, error), apply func(*ui.Bitmap, error)) bool {
	if o.Dispatch == nil {
		apply(load())
		return true
	}
	if b.imageLoads >= boardImageLoadLimit {
		return false
	}
	b.imageLoads++
	go func() {
		bitmap, err := load()
		o.Dispatch(func() {
			b.imageLoads--
			apply(bitmap, err)
		})
	}()
	return true
}
