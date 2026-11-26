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
	result, err := applyMask(src, []float32{-20, 20, -20, 20}, 2, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds() != src.Bounds() || result.NRGBAAt(0, 0).A != 0 || result.NRGBAAt(3, 1) != src.NRGBAAt(3, 1) {
		t.Fatalf("wrong original size/color/alpha: %v %+v %+v", result.Bounds(), result.NRGBAAt(0, 0), result.NRGBAAt(3, 1))
	}
}

func TestCutoutHandlesProbabilitiesAndDecontaminatesEdgeSpill(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 6, 1))
	// Solid red foreground on the left, semi-transparent edge with white spill in the middle, white background on the right.
	for x := range 6 {
		switch {
		case x < 3:
			src.SetNRGBA(x, 0, color.NRGBA{R: 220, G: 20, B: 20, A: 255})
		case x == 3:
			src.SetNRGBA(x, 0, color.NRGBA{R: 240, G: 180, B: 180, A: 255})
		default:
			src.SetNRGBA(x, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	probs := []float32{1, 1, 1, 0.35, 0, 0}
	result, err := applyMask(src, probs, 6, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.NRGBAAt(5, 0); got.A != 0 {
		t.Fatalf("probability 0 should stay transparent, got alpha %d", got.A)
	}
	if got := result.NRGBAAt(0, 0); got != (color.NRGBA{R: 220, G: 20, B: 20, A: 255}) {
		t.Fatalf("solid interior changed: %+v", got)
	}
	edge := result.NRGBAAt(3, 0)
	if edge.G >= 180 || edge.B >= 180 {
		t.Fatalf("expected edge background spill to be reduced toward red foreground, got %+v", edge)
	}
}
