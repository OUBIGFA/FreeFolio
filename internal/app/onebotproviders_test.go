package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/prefs"
)

func setupOneBot(a *App, key string) {
	a.prefs.AI = prefs.AI{Active: "test", Connections: []prefs.AIConnection{{ID: "test", Name: "Test", Vendor: "openai", Protocol: agent.Chat, BaseURL: "https://api.openai.com/v1", Model: "test-model", Models: []agent.ModelInfo{{ID: "test-model", Enabled: true}}}}}
	a.qa.secrets = memSecrets{}
	a.qa.keyRead = false
	a.qa.keyInput = key
	if key != "" {
		_ = a.saveAIKey()
	}
}

func TestOneBotProvidersKeepKeysAndModelChoicesSeparate(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "first-key")
	first := a.prefs.AI.Active
	a.addAIConnection("anthropic")
	second := a.prefs.AI.Active
	a.qa.keyInput = "second-key"
	if err := a.saveAIKey(); err != nil {
		t.Fatal(err)
	}
	a.prefs.AI.Current().Models = []agent.ModelInfo{{ID: "claude", Enabled: true}, {ID: "unused"}}
	a.prefs.AI.Current().Model = "claude"
	a.selectAIConnection(first)
	if s := a.qaSettings(); s.APIKey != "first-key" || s.Model != "test-model" {
		t.Fatalf("first: %+v", s)
	}
	a.selectAIConnection(second)
	if s := a.qaSettings(); s.APIKey != "second-key" || s.Protocol != agent.Anthropic {
		t.Fatalf("second: %+v", s)
	}
	a.savePrefs()
	raw, err := os.ReadFile(a.prefStore.Path)
	if err != nil || strings.Contains(string(raw), "-key") {
		t.Fatal("credentials escaped into preferences")
	}
	a.removeAIKey("second-key")
	if s := a.qaSettings(); s.APIKey != "" {
		t.Fatal("removed key is still in use")
	}
	a.selectAIConnection(first)
	if a.qaSettings().APIKey != "first-key" {
		t.Fatal("removing another key changed first provider")
	}
}

func TestOneBotDiscoveryPreservesSelectionOnFailureAndDropsStaleResult(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	a.qa.listModels = func(agent.Settings) ([]agent.ModelInfo, error) { return nil, errors.New("offline") }
	a.fetchModels()
	if a.prefs.AI.Current().Model != "test-model" || !a.prefs.AI.Current().Models[0].Enabled || a.qa.modelsErr == nil {
		t.Fatal("failed discovery discarded selected models")
	}
	a.qa.listModels = func(agent.Settings) ([]agent.ModelInfo, error) {
		a.prefs.AI.Current().BaseURL = "https://changed.example"
		return []agent.ModelInfo{{ID: "stale"}}, nil
	}
	a.fetchModels()
	if len(a.prefs.AI.Current().Models) != 1 || a.prefs.AI.Current().Models[0].ID != "test-model" {
		t.Fatal("stale service list accepted")
	}
}

func TestOneBotModelListSelectionAndManualAdd(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "key")
	a.qa.listModels = func(agent.Settings) ([]agent.ModelInfo, error) {
		return []agent.ModelInfo{{ID: "alpha"}, {ID: "beta"}}, nil
	}
	a.openSettings(tabAI)
	a.fetchModels()
	tt.Frame()
	tt.Scroll(680, 550, 0, 400)
	tt.Frame()
	if err := tt.Click("alpha"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	c := a.prefs.AI.Current()
	enabled := false
	for _, m := range c.Models {
		if m.ID == "alpha" {
			enabled = m.Enabled
		}
	}
	if !enabled {
		t.Fatal("checkbox did not enable model")
	}
	a.qa.manualName = "private-model"
	a.addManualAIModel()
	if c.CurrentModel() == nil || c.Model != "private-model" || !c.CurrentModel().Enabled {
		t.Fatal("manual model missing")
	}
	loaded, err := a.prefStore.Load()
	if err != nil || loaded.AI.Current().Model != "private-model" {
		t.Fatal("choices not persisted")
	}
}

func TestOneBotSecretFailureDoesNotDiscardInputOrSavedKey(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "saved")
	a.qa.secrets = failingSecrets{}
	a.qa.keyInput = "new"
	if err := a.saveAIKey(); err == nil || a.qa.keyInput != "new" || a.qaSettings().APIKey != "saved" {
		t.Fatal("failed save lost credential state")
	}
	a.removeAIKey("saved")
	if a.qaSettings().APIKey != "saved" {
		t.Fatal("failed removal lost saved key")
	}
}

// A connection keeps several keys: pasted together or added later, without
// duplicates, each removable on its own. Version 1 keyrings read as lists.
func TestOneBotMultipleKeysAndVersion1Keyring(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "")
	secrets := memSecrets{}
	secrets.Set(apiKeySecret, []byte(`{"version":1,"keys":{"test":"old-key","other":"kept"}}`))
	a.qa.secrets = secrets
	a.qa.keyRead = false
	if s := a.qaSettings(); s.APIKey != "old-key" || len(s.APIKeys) != 1 {
		t.Fatalf("version 1 key: %+v", s)
	}
	a.qa.keyInput = "k-two, k-three\nold-key"
	if err := a.saveAIKey(); err != nil {
		t.Fatal(err)
	}
	s := a.qaSettings()
	if strings.Join(s.APIKeys, " ") != "old-key k-two k-three" || s.APIKey != "old-key" {
		t.Fatalf("keys: %v", s.APIKeys)
	}
	a.removeAIKey("k-two")
	a.qa.keyRead = false
	if got := strings.Join(a.qaSettings().APIKeys, " "); got != "old-key k-three" {
		t.Fatalf("after removal: %s", got)
	}
	keys, err := a.readAIKeys()
	if err != nil || keys.Version != 2 || strings.Join(keys.Keys["other"], "") != "kept" {
		t.Fatalf("keyring: %+v %v", keys, err)
	}
}

// The settings show a saved key masked, in full on request, and offer the
// strategy once there are two keys.
func TestOneBotSettingsRevealKeyAndChooseStrategy(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "sk-first-secret-1234")
	a.openSettings(tabAI)
	tt.Frame()
	if tt.HasText("sk-first-secret-1234") || !tt.HasText(maskAPIKey("sk-first-secret-1234")) {
		t.Fatal("saved key must start masked")
	}
	if tt.HasText("多 Key 策略") {
		t.Fatal("one key needs no strategy")
	}
	if err := tt.Click("显示 API Key"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("sk-first-secret-1234") {
		t.Fatal("key not shown again")
	}
	a.qa.keyInput = "sk-second-secret-5678"
	a.saveAIKey()
	tt.Frame()
	if !tt.HasText("多 Key 策略") || !tt.HasText(maskAPIKey("sk-second-secret-5678")) {
		t.Fatal("second key or strategy missing", tt.Texts())
	}
}
