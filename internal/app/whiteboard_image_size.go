package app

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/editor"
)

// Resolve only default image sizing. Explicit user sizes remain authoritative.
// Providers with enumerated sizes retain their native reference/auto behavior
// instead of receiving a guessed WxH value that their API cannot accept.
func boardReferenceImageSize(s agent.Settings, req *agent.MediaRequest, images []editor.BoardAIImage) error {
	if req.Kind != agent.MediaImage || len(images) == 0 || req.Size != "" && req.Size != "auto" || req.AspectRatio != "" {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(images[0].Data))
	if err != nil {
		return fmt.Errorf("cannot read reference image size: %w", err)
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		return fmt.Errorf("invalid reference image dimensions")
	}
	model := strings.ToLower(strings.TrimPrefix(s.Model, "models/"))
	if s.Protocol == agent.Gemini {
		// Gemini documents input-sized output when size/aspect are omitted.
		req.Size = ""
		if max(w, h) > 4096 && !strings.Contains(model, "2.5-flash") {
			req.Size = "4K"
		}
		return nil
	}
	if strings.HasPrefix(model, "gpt-image-2") && float64(max(w, h))/float64(min(w, h)) <= 3 {
		ow, oh := fitGPTImageSize(w, h)
		req.Size = fmt.Sprintf("%dx%d", ow, oh)
	} else if s.Protocol == agent.Media || s.Protocol == agent.Responses {
		req.Size = "auto"
	}
	return nil
}

// Respect the published pixel range as well as the edge and 16-pixel grid
// constraints. Small inputs need a proportional increase to the minimum area.
func fitGPTImageSize(w, h int) (int, int) {
	const minPixels, maxPixels = 655360, 8294400
	pixels := float64(w) * float64(h)
	factor := math.Min(1, math.Min(3840/float64(max(w, h)), math.Sqrt(maxPixels/pixels)))
	factor = math.Max(factor, math.Sqrt(minPixels/pixels))
	ow, oh := max(16, int(math.Floor(float64(w)*factor/16))*16), max(16, int(math.Floor(float64(h)*factor/16))*16)
	for ow*oh < minPixels {
		if float64(ow)/float64(oh) < float64(w)/float64(h) {
			ow += 16
		} else {
			oh += 16
		}
	}
	if ow > oh*3 {
		oh = (ow/3 + 15) / 16 * 16
	}
	if oh > ow*3 {
		ow = (oh/3 + 15) / 16 * 16
	}
	return ow, oh
}
