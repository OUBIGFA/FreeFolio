package app

import (
	"bytes"
	"image/png"
	"testing"
)

// Each place gets the logo at its exact pixel size, its tile kept opaque
// at the center.
func TestIconPNGSizes(t *testing.T) {
	for _, size := range []int{16, 20, 24, 32, 48} {
		img, err := png.Decode(bytes.NewReader(iconPNG(size)))
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("size %d rendered %v", size, b)
		}
		if _, _, _, a := img.At(size/2, size/2).RGBA(); a == 0 {
			t.Fatalf("size %d: transparent center", size)
		}
	}
	if &iconPNG(16)[0] != &iconPNG(16)[0] {
		t.Fatal("renderings are kept")
	}
}
