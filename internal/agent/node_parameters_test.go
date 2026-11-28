package agent

import "testing"

func TestNodeParametersPreserveConnectionAndRejectUnsupported(t *testing.T) {
	s := Settings{Protocol: Responses, Parameters: Parameters{MaxOutput: 100}}
	next, err := s.WithNodeParameters(map[string]any{"text.max_output": 200.0, "text.temperature": .4, "text.reasoning": "high"})
	if err != nil || next.Parameters.MaxOutput != 200 || *next.Parameters.Temperature != .4 || s.Parameters.MaxOutput != 100 || s.Parameters.Temperature != nil {
		t.Fatalf("override leaked or missing: %+v %v", next, err)
	}
	for _, bad := range []map[string]any{{"text.temperature": 3.0}, {"text.max_output": 1.5}, {"text.reasoning": "invented"}, {"endpoint": "elsewhere"}} {
		if _, err := s.WithNodeParameters(bad); err == nil {
			t.Fatalf("invalid parameter accepted: %v", bad)
		}
	}
	s.Protocol = ChatGPT
	if _, err := s.WithNodeParameters(map[string]any{"text.temperature": 1.0}); err == nil {
		t.Fatal("ChatGPT temperature silently ignored")
	}
}
