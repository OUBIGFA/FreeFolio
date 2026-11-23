package agent

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// chatGPTModels keeps the account's order and uses only metadata returned for
// that account. The public API-key catalog describes a different entitlement.
func (s Settings) chatGPTModels(ctx context.Context) ([]ModelInfo, error) {
	data, err := s.readJSON(ctx, http.MethodGet, strings.TrimRight(s.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	items, ok := data["models"].([]any)
	if !ok {
		return nil, errors.New("ChatGPT returned an invalid model catalog")
	}
	models := []ModelInfo{}
	seen := map[string]bool{}
	for _, raw := range items {
		m := object(raw)
		if str(m["visibility"]) != "list" {
			continue
		}
		id, name := str(m["slug"]), str(m["display_name"])
		if strings.TrimSpace(id) == "" || len(id) > 200 || strings.TrimSpace(name) == "" || len(name) > 200 || seen[id] {
			return nil, errors.New("ChatGPT returned an invalid model entry")
		}
		seen[id] = true
		caps := Capabilities{Temperature: Unsupported}
		caps.Input = texts(m["input_modalities"])
		caps.Context = number(m["context_window"])
		caps.Tools = support(object(m["capabilities"])["tool_use"])
		caps.Reasoning = support(m["supports_reasoning_summaries"])
		for _, raw := range array(m["supported_reasoning_levels"]) {
			if effort := str(object(raw)["effort"]); effort != "" {
				caps.Efforts = append(caps.Efforts, effort)
			}
		}
		models = append(models, ModelInfo{ID: id, Name: name, Source: "ChatGPT account", Updated: time.Now().UTC().Format(time.RFC3339), Capabilities: caps})
	}
	return models, nil
}

func array(v any) []any { values, _ := v.([]any); return values }
