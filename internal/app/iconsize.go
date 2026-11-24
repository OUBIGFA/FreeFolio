package app

import (
	"bytes"
	"image"
	"image/png"
	"sync"

	"github.com/OUBIGFA/FreeFolio/resources"
	"golang.org/x/image/draw"
)

// The logo is a 1024 px picture. Handed whole to the tray or the window,
// the system shrinks it to 16–32 px by sampling, which leaves the small
// icons blurred and ragged, so each place gets the logo rendered at the
// exact pixel size it shows, filtered as it shrinks.

var (
	decodedResourceIcon = sync.OnceValues(func() (image.Image, error) {
		return png.Decode(bytes.NewReader(resources.Icon))
	})
	iconCache = struct {
		sync.Mutex
		sizes map[int][]byte
	}{sizes: map[int][]byte{}}
)

// iconPNG returns the logo as a PNG of size×size pixels, or the original
// logo should rendering fail. Renderings are kept by size.
func iconPNG(size int) []byte {
	iconCache.Lock()
	defer iconCache.Unlock()
	if data, ok := iconCache.sizes[size]; ok {
		return data
	}
	data := resources.Icon
	if src, err := decodedResourceIcon(); err == nil && size > 0 {
		dst := image.NewNRGBA(image.Rect(0, 0, size, size))
		// Catmull-Rom widens its kernel with the scale, so shrinking
		// 1024 px to 16 averages every source pixel instead of skipping
		// most of them.
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
		var buf bytes.Buffer
		if png.Encode(&buf, dst) == nil {
			data = buf.Bytes()
		}
	}
	iconCache.sizes[size] = data
	return data
}
