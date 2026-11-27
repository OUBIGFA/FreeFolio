package app

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
)

func TestBoardReferenceImageSizingRespectsProviderModes(t *testing.T) {
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 1600, 960)))
	images := []editor.BoardAIImage{{Data: data.Bytes(), MIME: "image/png"}}
	for _, tc := range []struct{ protocol, model, want string }{{agent.Media, "gpt-image-2", "1600x960"}, {agent.Media, "gpt-image-1", "auto"}, {agent.Gemini, "gemini-3-pro-image", ""}} {
		req := agent.MediaRequest{Kind: agent.MediaImage}
		if err := boardReferenceImageSize(agent.Settings{Protocol: tc.protocol, Model: tc.model}, &req, images); err != nil {
			t.Fatal(err)
		}
		if req.Size != tc.want || req.AspectRatio != "" {
			t.Fatalf("%s size %+v", tc.model, req)
		}
		req.Size = "1024x1024"
		if err := boardReferenceImageSize(agent.Settings{Protocol: tc.protocol, Model: tc.model}, &req, images); err != nil || req.Size != "1024x1024" {
			t.Fatal("manual size overwritten")
		}
	}
}

func TestGPTReferenceDimensionsMeetPixelLimits(t *testing.T) {
	for _, size := range [][2]int{{640, 960}, {128, 128}, {4000, 3000}, {8000, 4000}, {1536, 512}, {1600, 960}} {
		w, h := fitGPTImageSize(size[0], size[1])
		if w%16 != 0 || h%16 != 0 || w*h < 655360 || w*h > 8294400 || max(w, h) > 3840 || max(w, h) > 3*min(w, h) {
			t.Fatalf("%v produced invalid %dx%d", size, w, h)
		}
	}
}
