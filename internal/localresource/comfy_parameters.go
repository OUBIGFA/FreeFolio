package localresource

import (
	"errors"
	"math"
	"slices"
	"strings"
)

// ComfyOptions carries user controls shared by native workflows. Pointer fields
// distinguish an explicit zero/empty value from a legacy board's default.
type ComfyOptions struct {
	Seed                 *int64
	Steps                int
	Guidance             *float64
	NegativePrompt       *string
	ColorCorrection      string
	MaskThreshold        *float64
	Sensitivity          *float64
	MaskBlur, MaskOffset int
}

func firstComfyOptions(options []ComfyOptions) ComfyOptions {
	if len(options) > 0 {
		return options[0]
	}
	return ComfyOptions{}
}

func (o ComfyOptions) sampling(seed uint64, steps int, guidance float64) (uint64, int, float64, error) {
	if o.Seed != nil {
		if *o.Seed < 0 || *o.Seed > 9007199254740991 {
			return 0, 0, 0, errors.New("seed must be between 0 and 9007199254740991")
		}
		seed = uint64(*o.Seed)
	}
	if o.Steps != 0 {
		if o.Steps < 1 || o.Steps > 100 {
			return 0, 0, 0, errors.New("sampling steps must be between 1 and 100")
		}
		steps = o.Steps
	}
	if o.Guidance != nil {
		guidance = *o.Guidance
		if math.IsNaN(guidance) || math.IsInf(guidance, 0) || guidance < 0 || guidance > 20 {
			return 0, 0, 0, errors.New("guidance must be between 0 and 20")
		}
	}
	return seed, steps, guidance, nil
}

func (o ComfyOptions) negative(fallback string) string {
	if o.NegativePrompt != nil {
		return *o.NegativePrompt
	}
	return fallback
}

func validUnitValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func musicMetadata(bpm int, key, meter string) (int, string, string, error) {
	if bpm == 0 {
		bpm = 120
	}
	if key == "" {
		key = "C major"
	}
	if meter == "" {
		meter = "4"
	}
	note, mode, ok := strings.Cut(key, " ")
	if bpm < 10 || bpm > 300 || !ok || !slices.Contains([]string{"C", "C#", "Db", "D", "D#", "Eb", "E", "F", "F#", "Gb", "G", "G#", "Ab", "A", "A#", "Bb", "B"}, note) || !slices.Contains([]string{"major", "minor"}, mode) || !slices.Contains([]string{"2", "3", "4", "6"}, meter) {
		return 0, "", "", errors.New("invalid music tempo, key or time signature")
	}
	return bpm, key, meter, nil
}

// Geometry follows Wan's 32-pixel grid and 4n+1 latent frame layout. Limits are
// application resource bounds, not a claim about the model's full capability.
func localVideoGeometry(size string, fps int) (int, int, int, error) {
	if fps == 0 {
		fps = 24
	}
	if !slices.Contains([]int{12, 16, 24, 30}, fps) {
		return 0, 0, 0, errors.New("video frame rate must be 12, 16, 24 or 30")
	}
	sizes := map[string][2]int{"": {832, 480}, "832x480": {832, 480}, "480x832": {480, 832}, "640x640": {640, 640}, "1280x704": {1280, 704}, "704x1280": {704, 1280}}
	wh, ok := sizes[size]
	if !ok {
		return 0, 0, 0, errors.New("unsupported local video size")
	}
	return wh[0], wh[1], fps, nil
}
