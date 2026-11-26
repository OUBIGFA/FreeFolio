package localresource

import (
	"image"
	"image/color"
	"testing"
)

func TestCutoutPreservesOriginalColorSizeAndTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for column := range 4 {
		for row := range 2 {
			src.SetNRGBA(column, row, color.NRGBA{R: 200, G: 40, B: 20, A: 128})
		}
	}
	result, err := applyMask(src, []float32{-20, 20, -20, 20}, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds() != src.Bounds() || result.NRGBAAt(0, 0).A != 0 || result.NRGBAAt(3, 1) != src.NRGBAAt(3, 1) {
		t.Fatalf("wrong original size/color/alpha: %v %+v %+v", result.Bounds(), result.NRGBAAt(0, 0), result.NRGBAAt(3, 1))
	}
}
