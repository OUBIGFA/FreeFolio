package localresource

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestUpscaleCatalogAndRuntimeChecksums(t *testing.T) {
	if len(UpscaleModels) != 2 {
		t.Fatalf("expected 2 upscale models, got %d", len(UpscaleModels))
	}
	for _, id := range []string{"realesrgan-x4plus", "realesrgan-x4plus-anime"} {
		if _, ok := UpscaleModel(id); !ok {
			t.Fatalf("missing upscale model %q", id)
		}
	}
	rt, ok := UpscaleRuntime()
	if !ok {
		t.Skip("upscale runtime not defined on this platform")
	}
	if !strings.HasPrefix(rt.URL, "https://") || len(rt.SHA) != 64 || rt.Size <= 0 || rt.member == "" {
		t.Fatalf("invalid upscale runtime metadata: %+v", rt)
	}
}

func TestResizePNGSupersamplesWithAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			src.SetNRGBA(x, y, color.NRGBA{R: 40, G: 160, B: 220, A: 180})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	out, err := resizePNG(buf.Bytes(), 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 4 || got.Bounds().Dy() != 2 {
		t.Fatalf("resized dimensions = %v, want 4x2", got.Bounds())
	}
	_, _, _, a := got.At(2, 1).RGBA()
	if a == 0 || a == 0xffff {
		t.Fatalf("expected preserved semi-transparent alpha, got %d", a)
	}
}

func TestUpscaleRejectsInvalidArguments(t *testing.T) {
	root := t.TempDir()
	if _, err := Upscale(context.Background(), root, "unknown", 4, []byte("x")); err == nil {
		t.Fatal("accepted unknown upscale model")
	}
	if _, err := Upscale(context.Background(), root, "realesrgan-x4plus", 3, []byte("x")); err == nil {
		t.Fatal("accepted unsupported scale factor 3")
	}
}
