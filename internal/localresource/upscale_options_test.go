package localresource

import (
	"slices"
	"testing"
)

func TestUpscaleNativeOptionsUseArgumentList(t *testing.T) {
	args, err := upscaleArgs(map[string]any{"upscale.tile_size": float64(256), "upscale.gpu": float64(0), "upscale.tta": true, "upscale.threads": "2:3:2"})
	if err != nil || !slices.Contains(args, "-x") || !slices.Contains(args, "256") || !slices.Contains(args, "2:3:2") {
		t.Fatalf("settings ignored: %v %v", args, err)
	}
	for _, bad := range []map[string]any{{"upscale.threads": "1:2:2 -o somewhere"}, {"upscale.tile_size": float64(1)}, {"upscale.gpu": -3.0}, {"output": "elsewhere"}} {
		if _, err := upscaleArgs(bad); err == nil {
			t.Fatalf("invalid options accepted: %v", bad)
		}
	}
}
