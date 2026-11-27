package localresource

import (
	"encoding/json"
	"testing"
)

func comfyTextGenerateRequired(t *testing.T) map[string][]json.RawMessage {
	// The native TextGenerateV2 sampling mode requires these flattened inputs.
	var spec map[string][]json.RawMessage
	err := json.Unmarshal([]byte(`{"sampling_mode":["COMFY_DYNAMICCOMBO_V3",{"options":[{"key":"on","inputs":{"required":{"temperature":["FLOAT",{"default":0.7}],"top_k":["INT",{"default":64}],"top_p":["FLOAT",{"default":0.95}],"min_p":["FLOAT",{"default":0.05}],"repetition_penalty":["FLOAT",{"default":1.05}],"presence_penalty":["FLOAT",{"default":0}],"seed":["INT",{"default":0}]}}},{"key":"off","inputs":{"required":{}}}]}],"dtype":["COMBO",{"options":["default","bf16"]}]}`), &spec)
	if err != nil {
		t.Fatal(err)
	}
	delete(spec, "dtype")
	return spec
}

func TestComfyDynamicComboIncludesRequiredBranchValues(t *testing.T) {
	spec := comfyTextGenerateRequired(t)
	spec["dtype"] = []json.RawMessage{json.RawMessage(`"COMBO"`), json.RawMessage(`{"options":["default","bf16"]}`)}
	got := parseComfyRequiredDefaults(spec)
	for k, want := range map[string]any{"sampling_mode": "on", "sampling_mode.temperature": .7, "sampling_mode.top_k": 64.0, "sampling_mode.top_p": .95, "sampling_mode.min_p": .05, "sampling_mode.repetition_penalty": 1.05, "sampling_mode.presence_penalty": 0.0, "sampling_mode.seed": 0.0, "dtype": "default"} {
		if got[k] != want {
			t.Errorf("%s=%v, want %v", k, got[k], want)
		}
	}
}
