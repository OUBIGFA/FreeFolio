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
		_ = a.qa.saveAIKey()
	}
}

func TestOneBotProvidersKeepKeysAndModelChoicesSeparate(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "first-key")
	first := a.prefs.AI.Active
	a.qa.addAIConnection("anthropic")
	second := a.prefs.AI.Active
	a.qa.keyInput = "second-key"
	if err := a.qa.saveAIKey(); err != nil {
		t.Fatal(err)
	}
	a.prefs.AI.Current().Models = []agent.ModelInfo{{ID: "claude", Enabled: true}, {ID: "unused"}}
	a.prefs.AI.Current().Model = "claude"
	a.qa.selectAIConnection(first)
	if s := a.qa.qaSettings(); s.APIKey != "first-key" || s.Model != "test-model" {
		t.Fatalf("first: %+v", s)
	}
	a.qa.selectAIConnection(second)
	if s := a.qa.qaSettings(); s.APIKey != "second-key" || s.Protocol != agent.Anthropic {
		t.Fatalf("second: %+v", s)
	}
	a.savePrefs()
	raw, err := os.ReadFile(a.prefStore.Path)
	if err != nil || strings.Contains(string(raw), "-key") {
		t.Fatal("credentials escaped into preferences")
	}
	a.qa.removeAIKey("second-key")
	if s := a.qa.qaSettings(); s.APIKey != "" {
		t.Fatal("removed key is still in use")
	}
	a.qa.selectAIConnection(first)
	if a.qa.qaSettings().APIKey != "first-key" {
		t.Fatal("removing another key changed first provider")
	}
}

func TestOneBotDiscoveryPreservesSelectionOnFailureAndDropsStaleResult(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "key")
	a.qa.listModels = func(agent.Settings) ([]agent.ModelInfo, error) { return nil, errors.New("offline") }
	a.qa.fetchModels()
	if a.prefs.AI.Current().Model != "test-model" || !a.prefs.AI.Current().Models[0].Enabled || a.qa.modelsErr == nil {
		t.Fatal("failed discovery discarded selected models")
	}
	a.qa.listModels = func(agent.Settings) ([]agent.ModelInfo, error) {
		a.prefs.AI.Current().BaseURL = "https://changed.example"
		return []agent.ModelInfo{{ID: "stale"}}, nil
	}
	a.qa.fetchModels()
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
	a.qa.fetchModels()
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
	a.qa.addManualAIModel()
	if c.CurrentModel() == nil || c.Model != "private-model" || !c.CurrentModel().Enabled {
		t.Fatal("manual model missing")
	}
	loaded, err := a.prefStore.Load()
	if err != nil || loaded.AI.Current().Model != "private-model" {
		t.Fatal("choices not persisted")
	}
}

func TestOneBotClearProviderModelsIgnoresSearchAndPreservesKeys(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "saved-key")
	a.prefs.AI.Connections = append(a.prefs.AI.Connections, prefs.AIConnection{ID: "other", Models: []agent.ModelInfo{{ID: "keep"}}})
	conn := a.prefs.AI.Current()
	conn.Models = append(conn.Models, agent.ModelInfo{ID: "hidden", Source: "user"})
	a.openSettings(tabAI)
	a.qa.modelSearch = "test-model"
	tt.Frame()
	tt.Scroll(680, 550, 0, 400)
	tt.Frame()
	if err := tt.Click("清空模型"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(conn.Models) == 0 {
		t.Fatal("cleared before confirmation")
	}
	if err := tt.Click("确认清空模型"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	saved, err := a.prefStore.Load()
	if err != nil || len(saved.AI.Current().Models) != 0 || saved.AI.Current().Model != "" || len(saved.AI.Find("other").Models) != 1 || a.qa.qaSettings().APIKey != "saved-key" {
		t.Fatalf("cleared wrong provider data: %+v %v", saved.AI, err)
	}
}

func TestOneBotSecretFailureDoesNotDiscardInputOrSavedKey(t *testing.T) {
	a, _ := newTestApp(t)
	setupOneBot(a, "saved")
	a.qa.secrets = failingSecrets{}
	a.qa.keyInput = "new"
	if err := a.qa.saveAIKey(); err == nil || a.qa.keyInput != "new" || a.qa.qaSettings().APIKey != "saved" {
		t.Fatal("failed save lost credential state")
	}
	a.qa.removeAIKey("saved")
	if a.qa.qaSettings().APIKey != "saved" {
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
	if s := a.qa.qaSettings(); s.APIKey != "old-key" || len(s.APIKeys) != 1 {
		t.Fatalf("version 1 key: %+v", s)
	}
	a.qa.keyInput = "k-two, k-three\nold-key"
	if err := a.qa.saveAIKey(); err != nil {
		t.Fatal(err)
	}
	s := a.qa.qaSettings()
	if strings.Join(s.APIKeys, " ") != "old-key k-two k-three" || s.APIKey != "old-key" {
		t.Fatalf("keys: %v", s.APIKeys)
	}
	a.qa.removeAIKey("k-two")
	a.qa.keyRead = false
	if got := strings.Join(a.qa.qaSettings().APIKeys, " "); got != "old-key k-three" {
		t.Fatalf("after removal: %s", got)
	}
	keys, err := a.qa.readAIKeys()
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
	if err := tt.Click("供应商与连接"); err != nil {
		t.Fatal(err)
	}
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
	a.qa.saveAIKey()
	tt.Frame()
	if !tt.HasText("多 Key 策略") || !tt.HasText(maskAPIKey("sk-second-secret-5678")) {
		t.Fatal("second key or strategy missing", tt.Texts())
	}
}

func TestOneBotSettingsTabsOrderAndSwitching(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "sk-tab-order-1234")
	a.openSettings(tabAI)
	tt.Frame()
	modelsR, okM := tt.Find("模型")
	paramsR, okP := tt.Find("生成参数")
	providerR, okS := tt.Find("供应商与连接")
	if !okM || !okP || !okS || !(modelsR.X < paramsR.X && paramsR.X < providerR.X) {
		t.Fatalf("unexpected OneBot tab order: models=%+v(%v) params=%+v(%v) provider=%+v(%v)", modelsR, okM, paramsR, okP, providerR, okS)
	}
	if !tt.HasText("获取模型") || tt.HasText("服务地址") {
		t.Fatalf("default OneBot tab should be models: %v", tt.Texts())
	}
	if err := tt.Click("生成参数"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("输出上限") || tt.HasText("获取模型") {
		t.Fatalf("params tab missing controls: %v", tt.Texts())
	}
	if err := tt.Click("供应商与连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("服务地址") || !tt.HasText("粘贴 API Key") {
		t.Fatalf("provider tab missing controls: %v", tt.Texts())
	}
}
