package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelDiscoveryKeepsCapabilitiesAndPages(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			t.Error("missing native authentication/version")
		}
		if r.URL.Query().Get("after_id") == "" {
			w.Write([]byte(`{"data":[{"id":"alpha","display_name":"Alpha","max_input_tokens":10000,"max_tokens":2000,"capabilities":{"thinking":{"supported":true},"vision":{"supported":true}}}],"has_more":true,"last_id":"alpha"}`))
		} else {
			w.Write([]byte(`{"data":[{"id":"beta","display_name":"Beta"}],"has_more":false}`))
		}
	}))
	defer srv.Close()
	models, err := (Settings{BaseURL: srv.URL + "/v1", Protocol: Anthropic, APIKey: "test-key"}).DiscoverModels(context.Background())
	if err != nil || requests != 2 || len(models) != 2 {
		t.Fatalf("discovery: %+v, %v, requests %d", models, err, requests)
	}
	if models[0].ID != "alpha" || models[0].Capabilities.Context != 10000 || models[0].Capabilities.Reasoning != Supported || !models[0].Capabilities.Accepts("image") {
		t.Fatalf("metadata lost: %+v", models[0])
	}
	if models[1].Capabilities.Reasoning != Unknown {
		t.Fatal("missing capability must remain unknown")
	}
}

func TestModelCatalogDoesNotInventUnknownCapabilities(t *testing.T) {
	data := []byte(`{"openai":{"models":{"model-a":{"id":"model-a","name":"Model A","tool_call":true,"reasoning":false,"temperature":false,"modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":128000,"output":8000}}}}}`)
	models, err := ParseCatalog(data, "openai")
	if err != nil || len(models) != 1 || models[0].Capabilities.Tools != Supported || models[0].Capabilities.Temperature != Unsupported {
		t.Fatalf("catalog: %+v %v", models, err)
	}
	merged := MergeModelInfo(ModelInfo{ID: "model-a", Capabilities: Capabilities{Context: 64000}, Source: "provider"}, models[0])
	if merged.Capabilities.Context != 64000 || merged.Capabilities.Tools != Supported {
		t.Fatalf("provider data must win: %+v", merged)
	}
	var encoded map[string]any
	b, _ := json.Marshal(merged)
	json.Unmarshal(b, &encoded)
	if encoded["id"] != "model-a" {
		t.Fatal("model identity lost")
	}
}

func TestModelCatalogKeepsGeminiImageModels(t *testing.T) {
	// models.dev now gives native image models their own type, alongside chat.
	data := []byte(`{"google":{"models":{
		"gemini-3-pro-image":{"id":"gemini-3-pro-image","type":"image","modalities":{"input":["text","image"],"output":["text","image"]}},
		"gemini-chat":{"id":"gemini-chat","type":"chat","modalities":{"output":["text"]}},
		"embedding":{"id":"embedding","type":"embedding","modalities":{"output":["embedding"]}}
	}}}`)
	models, err := ParseCatalog(data, "google")
	if err != nil || len(models) != 2 {
		t.Fatalf("image catalog entries lost: %+v, %v", models, err)
	}
	for _, m := range models {
		if got := SupportsMedia(Gemini, "google", m.Capabilities, MediaImage); got != (m.ID == "gemini-3-pro-image") {
			t.Fatalf("wrong image capability: %+v", m)
		}
	}
}
