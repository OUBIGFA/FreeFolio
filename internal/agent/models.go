package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	Chat      = "openai-chat"
	Responses = "openai-responses"
	ChatGPT   = "chatgpt"
	Anthropic = "anthropic"
	Gemini    = "gemini"
	Ollama    = "ollama"
)

// Support is tri-state: missing metadata must not become a false capability.
type Support string

const (
	Unknown     Support = ""
	Supported   Support = "yes"
	Unsupported Support = "no"
)

type Capabilities struct {
	Tools            Support  `json:"tools,omitempty"`
	Reasoning        Support  `json:"reasoning,omitempty"`
	Temperature      Support  `json:"temperature,omitempty"`
	StructuredOutput Support  `json:"structuredOutput,omitempty"`
	Input            []string `json:"input,omitempty"`
	Output           []string `json:"output,omitempty"`
	Context          int      `json:"context,omitempty"`
	MaxOutput        int      `json:"maxOutput,omitempty"`
	Efforts          []string `json:"efforts,omitempty"`
}

func (c Capabilities) Accepts(modality string) bool { return slices.Contains(c.Input, modality) }

// Parameters belong to a connection; nil temperature means provider default.
type Parameters struct {
	MaxOutput      int      `json:"maxOutput,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
	Reasoning      string   `json:"reasoning,omitempty"`
	ThinkingBudget int      `json:"thinkingBudget,omitempty"`
}

type ModelInfo struct {
	ID           string       `json:"id"`
	Name         string       `json:"name,omitempty"`
	Enabled      bool         `json:"enabled,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	Source       string       `json:"source,omitempty"`
	Updated      string       `json:"updated,omitempty"`
	UserOverride bool         `json:"userOverride,omitempty"`
}

func support(v any) Support {
	if b, ok := v.(bool); ok {
		if b {
			return Supported
		}
		return Unsupported
	}
	if m, ok := v.(map[string]any); ok {
		return support(m["supported"])
	}
	return Unknown
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func number(v any) int            { f, _ := v.(float64); return int(f) }
func str(v any) string            { s, _ := v.(string); return s }
func texts(v any) []string {
	var out []string
	if a, ok := v.([]any); ok {
		for _, v := range a {
			if s := str(v); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// MergeModelInfo fills only unknown fields. The caller supplies the authority
// first: user overrides, then service metadata, then the public catalog.
func MergeModelInfo(primary, fallback ModelInfo) ModelInfo {
	if primary.ID != fallback.ID {
		return primary
	}
	if primary.UserOverride {
		return primary
	}
	if primary.Name == "" {
		primary.Name = fallback.Name
	}
	a, b := &primary.Capabilities, fallback.Capabilities
	if a.Tools == Unknown {
		a.Tools = b.Tools
	}
	if a.Reasoning == Unknown {
		a.Reasoning = b.Reasoning
	}
	if a.Temperature == Unknown {
		a.Temperature = b.Temperature
	}
	if a.StructuredOutput == Unknown {
		a.StructuredOutput = b.StructuredOutput
	}
	if len(a.Input) == 0 {
		a.Input = slices.Clone(b.Input)
	}
	if len(a.Output) == 0 {
		a.Output = slices.Clone(b.Output)
	}
	if a.Context == 0 {
		a.Context = b.Context
	}
	if a.MaxOutput == 0 {
		a.MaxOutput = b.MaxOutput
	}
	if len(a.Efforts) == 0 {
		a.Efforts = slices.Clone(b.Efforts)
	}
	if fallback.Source != "" && !strings.Contains(primary.Source, fallback.Source) {
		primary.Source = strings.Trim(primary.Source+"; "+fallback.Source, "; ")
	}
	return primary
}

func normalizeModels(models []ModelInfo) []ModelInfo {
	byID := map[string]ModelInfo{}
	for _, m := range models {
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" {
			continue
		}
		if old, ok := byID[m.ID]; ok {
			m = MergeModelInfo(old, m)
		}
		byID[m.ID] = m
	}
	out := make([]ModelInfo, 0, len(byID))
	for _, m := range byID {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s Settings) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 120 * time.Second}
}

func (s Settings) request(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	if s.Protocol == ChatGPT {
		if err := s.CheckService(); err != nil {
			return nil, err
		}
		base := strings.TrimRight(s.BaseURL, "/")
		if !(method == http.MethodGet && endpoint == base+"/models" || method == http.MethodPost && endpoint == base+"/responses") {
			return nil, ErrBadAddress
		}
		token, err := s.TokenSource.AccessToken(ctx)
		if err != nil {
			return nil, err
		}
		s.APIKey = token
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch s.Protocol {
	case Anthropic:
		req.Header.Set("x-api-key", s.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case Gemini:
		req.Header.Set("x-goog-api-key", s.APIKey)
	default:
		if s.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+s.APIKey)
		}
	}
	client := *s.httpClient()
	if s.Protocol == ChatGPT {
		// An OAuth grant is restricted to the official resource. Never forward
		// its bearer token to a redirected endpoint, including the same host.
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		err := apiError(res, s.APIKey)
		if api, ok := err.(*APIError); ok {
			api.Protocol = s.Protocol
		}
		return nil, err
	}
	return res, nil
}

func (s Settings) readJSON(ctx context.Context, method, endpoint string, body any) (map[string]any, error) {
	res, err := s.request(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<20 {
		return nil, errors.New("model metadata response is too large")
	}
	var out map[string]any
	err = json.Unmarshal(data, &out)
	return out, err
}

// DiscoverModels reads the native service endpoint, including all bounded
// pagination. It never infers capabilities from a model's marketing name.
func (s Settings) DiscoverModels(ctx context.Context) ([]ModelInfo, error) {
	if err := s.CheckService(); err != nil {
		return nil, err
	}
	if s.Protocol == ChatGPT {
		return s.chatGPTModels(ctx)
	}
	base := strings.TrimRight(s.BaseURL, "/")
	endpoint := base + "/models"
	if s.Protocol == Ollama {
		endpoint = base + "/api/tags"
	}
	var models []ModelInfo
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		if seen[endpoint] {
			return nil, errors.New("model pagination repeated a cursor")
		}
		seen[endpoint] = true
		data, err := s.readJSON(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		field := "data"
		if s.Protocol == Gemini || s.Protocol == Ollama {
			field = "models"
		}
		items, ok := data[field].([]any)
		if !ok {
			return nil, errors.New("service returned an invalid model list")
		}
		for _, raw := range items {
			m := object(raw)
			id := str(m["id"])
			if id == "" {
				id = str(m["name"])
			}
			if s.Protocol == Gemini {
				id = strings.TrimPrefix(id, "models/")
			}
			if id == "" {
				return nil, errors.New("service returned a model without an ID")
			}
			info := ModelInfo{ID: id, Name: str(m["display_name"]), Source: base, Updated: time.Now().UTC().Format(time.RFC3339)}
			if info.Name == "" {
				info.Name = str(m["displayName"])
			}
			if info.Name == "" {
				info.Name = str(m["name"])
			}
			c := &info.Capabilities
			c.Context = number(m["context_length"])
			c.MaxOutput = number(object(m["top_provider"])["max_completion_tokens"])
			if n := number(m["max_input_tokens"]); n > 0 {
				c.Context = n
			}
			if n := number(m["max_tokens"]); n > 0 {
				c.MaxOutput = n
			}
			if n := number(m["inputTokenLimit"]); n > 0 {
				c.Context = n
			}
			if n := number(m["outputTokenLimit"]); n > 0 {
				c.MaxOutput = n
			}
			caps := object(m["capabilities"])
			c.Reasoning = support(caps["thinking"])
			c.Tools = support(caps["tool_use"])
			c.StructuredOutput = support(caps["structured_outputs"])
			arch := object(m["architecture"])
			c.Input = texts(arch["input_modalities"])
			c.Output = texts(arch["output_modalities"])
			if support(caps["vision"]) == Supported {
				c.Input = []string{"text", "image"}
			}
			if params, exists := m["supported_parameters"]; exists {
				p := texts(params)
				c.Tools = Unsupported
				c.Temperature = Unsupported
				c.Reasoning = Unsupported
				c.StructuredOutput = Unsupported
				for _, k := range p {
					switch k {
					case "tools":
						c.Tools = Supported
					case "temperature":
						c.Temperature = Supported
					case "reasoning", "reasoning_effort":
						c.Reasoning = Supported
					case "structured_outputs", "response_format":
						c.StructuredOutput = Supported
					}
				}
			}
			if s.Protocol == Gemini {
				methods := texts(m["supportedGenerationMethods"])
				if !slices.Contains(methods, "generateContent") {
					continue
				}
				// generateContent alone does not specify output modalities;
				// preserve unknown until official metadata/catalog supplies them.
			}
			if s.Protocol == Ollama {
				show, err := s.readJSON(ctx, http.MethodPost, base+"/api/show", map[string]any{"model": id})
				if err != nil {
					return nil, fmt.Errorf("model %s: %w", id, err)
				}
				for _, cap := range texts(show["capabilities"]) {
					switch cap {
					case "tools":
						c.Tools = Supported
					case "thinking":
						c.Reasoning = Supported
					case "vision":
						c.Input = []string{"text", "image"}
					case "completion":
						c.Output = []string{"text"}
					}
				}
				for key, v := range object(show["model_info"]) {
					if strings.HasSuffix(key, ".context_length") {
						c.Context = number(v)
					}
				}
			}
			models = append(models, info)
		}
		next := str(data["nextPageToken"])
		if next != "" {
			endpoint = base + "/models?pageToken=" + url.QueryEscape(next)
			continue
		}
		if more, _ := data["has_more"].(bool); more {
			cursor := str(data["last_id"])
			if cursor == "" {
				return nil, errors.New("model pagination has no cursor")
			}
			endpoint = base + "/models?after_id=" + url.QueryEscape(cursor)
			continue
		}
		return normalizeModels(models), nil
	}
	return nil, errors.New("model pagination exceeded 100 pages")
}

// ParseCatalog uses the public models.dev schema, also used by OpenCode. Only
// exact provider/model matches are used, never fuzzy names or inferred flags.
func ParseCatalog(data []byte, vendor string) ([]ModelInfo, error) {
	var catalog map[string]struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	provider, ok := catalog[vendor]
	if !ok {
		return nil, fmt.Errorf("no catalog entry for %s", vendor)
	}
	var out []ModelInfo
	for id, raw := range provider.Models {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		if typ := str(m["type"]); typ != "" && typ != "chat" {
			continue
		}
		lim, modal := object(m["limit"]), object(m["modalities"])
		c := Capabilities{Tools: support(m["tool_call"]), Reasoning: support(m["reasoning"]), Temperature: support(m["temperature"]), StructuredOutput: support(m["structured_output"]), Input: texts(modal["input"]), Output: texts(modal["output"]), Context: number(lim["context"]), MaxOutput: number(lim["output"])}
		if opts, ok := m["reasoning_options"].([]any); ok {
			for _, opt := range opts {
				if str(object(opt)["type"]) == "effort" {
					c.Efforts = texts(object(opt)["values"])
				}
			}
		}
		out = append(out, ModelInfo{ID: id, Name: str(m["name"]), Capabilities: c, Source: "https://models.dev", Updated: str(m["last_updated"])})
	}
	return normalizeModels(out), nil
}

func FetchCatalog(ctx context.Context, vendor string) ([]ModelInfo, error) {
	s := Settings{}
	res, err := s.request(ctx, http.MethodGet, "https://models.dev/api.json", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 32<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 32<<20 {
		return nil, errors.New("model catalog is too large")
	}
	return ParseCatalog(data, vendor)
}
