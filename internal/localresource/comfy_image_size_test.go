package localresource

import (
	"image/color"
	"testing"
)

func TestComfyReferenceSizePreservesPixelsAndScalesTogether(t *testing.T) {
	for _, tc := range []struct{ w, h, wantW, wantH int }{{640, 960, 640, 960}, {1920, 1080, 1920, 1088}, {4096, 2048, 2048, 1024}, {4096, 512, 2048, 256}, {256, 512, 256, 512}} {
		data := comfyTestPNG(t, tc.w, tc.h, color.NRGBA{A: 255})
		w, h := resolveComfyImageSize("", "", data, 32)
		if w != tc.wantW || h != tc.wantH {
			t.Fatalf("%dx%d became %dx%d, want %dx%d", tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
	}
}
