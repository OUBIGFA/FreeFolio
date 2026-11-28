package localresource

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// User tuning adds separate argv entries; paths and output format remain owned
// by the worker so node parameters cannot redirect files or become shell code.
func upscaleArgs(values map[string]any) ([]string, error) {
	controls := NativeControls("local-ncnn", "", nil)
	for k, v := range values {
		i := slices.IndexFunc(controls, func(c NativeControl) bool { return c.Key == k })
		if i < 0 {
			return nil, fmt.Errorf("unsupported upscale parameter: %s", k)
		}
		if err := controls[i].Validate(v); err != nil {
			return nil, err
		}
	}
	var args []string
	if v, ok := values["upscale.tile_size"].(float64); ok {
		if v != 0 && (v < 32 || int(v)%32 != 0) {
			return nil, fmt.Errorf("tile size must be zero or a multiple of 32")
		}
		args = append(args, "-t", strconv.Itoa(int(v)))
	}
	if v, ok := values["upscale.gpu"].(float64); ok && v >= 0 {
		args = append(args, "-g", strconv.Itoa(int(v)))
	}
	if v, ok := values["upscale.tta"].(bool); ok && v {
		args = append(args, "-x")
	}
	if v, ok := values["upscale.threads"].(string); ok {
		parts := strings.Split(v, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("threads must be load:process:save")
		}
		for _, p := range parts {
			n, e := strconv.Atoi(p)
			if e != nil || n < 1 || n > 64 {
				return nil, fmt.Errorf("each thread count must be between 1 and 64")
			}
		}
		args = append(args, "-j", v)
	}
	return args, nil
}
