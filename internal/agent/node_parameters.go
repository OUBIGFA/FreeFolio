package agent

import (
	"fmt"
	"math"
	"slices"
)

// NodeParameter is a supported per-request override. It has no credentials,
// endpoint, tools or global preferences; those remain connection-owned.
type NodeParameter struct {
	Key, Label string
	Default    any
	Max        float64
	Choices    []string
}

// NodeParameters describes overrides supported by the connection protocol and model.
func NodeParameters(protocol, vendor string, c Capabilities, p Parameters) []NodeParameter {
	var out []NodeParameter
	if protocol != ChatGPT {
		limit := c.MaxOutput
		if limit == 0 {
			limit = 1048576
		}
		out = append(out, NodeParameter{Key: "text.max_output", Label: "输出长度上限", Default: float64(p.MaxOutput), Max: float64(limit)})
		if c.Temperature != Unsupported {
			v := 1.0
			if p.Temperature != nil {
				v = *p.Temperature
			}
			out = append(out, NodeParameter{Key: "text.temperature", Label: "生成温度", Default: v, Max: 2})
		}
	}
	if c.Reasoning != Unsupported {
		choices := append([]string{"", "off"}, c.Efforts...)
		if len(c.Efforts) == 0 {
			choices = append(choices, "low", "medium", "high")
		}
		out = append(out, NodeParameter{Key: "text.reasoning", Label: "推理强度", Default: p.Reasoning, Choices: choices})
		if protocol == Anthropic || protocol == Gemini || protocol == Chat && vendor == "openrouter" {
			out = append(out, NodeParameter{Key: "text.thinking_budget", Label: "思考长度上限", Default: float64(p.ThinkingBudget), Max: 1048576})
		}
	}
	return out
}

// WithNodeParameters copies settings and rejects unsupported overrides before
// constructing the provider, preserving the connection's defaults for other nodes.
func (s Settings) WithNodeParameters(values map[string]any) (Settings, error) {
	fields := NodeParameters(s.Protocol, s.Vendor, s.Capabilities, s.Parameters)
	for key, value := range values {
		i := slices.IndexFunc(fields, func(p NodeParameter) bool { return p.Key == key })
		if i < 0 {
			return s, fmt.Errorf("unsupported text model parameter: %s", key)
		}
		f := fields[i]
		if len(f.Choices) > 0 {
			v, ok := value.(string)
			if !ok || !slices.Contains(f.Choices, v) {
				return s, fmt.Errorf("invalid text model parameter: %s", key)
			}
			s.Parameters.Reasoning = v
			continue
		}
		v, ok := value.(float64)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > f.Max || key != "text.temperature" && math.Trunc(v) != v {
			return s, fmt.Errorf("invalid text model parameter: %s", key)
		}
		switch key {
		case "text.max_output":
			s.Parameters.MaxOutput = int(v)
		case "text.temperature":
			s.Parameters.Temperature = &v
		case "text.thinking_budget":
			s.Parameters.ThinkingBudget = int(v)
		}
	}
	return s, nil
}
