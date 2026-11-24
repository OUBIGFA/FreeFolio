package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/OUBIGFA/FreeFolio/internal/chatgpt"
)

func TestChatGPTSettingsShowsAccountActionsWithoutAPIKey(t *testing.T) {
	a, tt := newTestApp(t)
	a.addAIConnection("chatgpt")
	a.openSettings(tabAI)
	tt.SetSize(1200, 900)
	tt.Frame()
	if !tt.HasText("使用 ChatGPT 继续") || tt.HasText("API Key") || tt.HasText("接口协议") || tt.HasText("服务地址") {
		t.Fatalf("wrong auth controls: %v", tt.Texts())
	}
	if !errors.Is(a.qaSettings().CheckService(), agent.ErrChatGPTSignIn) {
		t.Fatal("unsigned connection ready")
	}
	if err := tt.Click("使用 ChatGPT 继续"); err != nil {
		t.Fatal(err)
	}
	// A test app has no browser opener. It must report this, never open the
	// user's browser or create a fake connected state.
	if a.chatGPTSession().Status == chatgpt.Connected {
		t.Fatal("fake login success")
	}
}

func TestChatGPTConnectionSwitchAndWelcomePersistence(t *testing.T) {
	a, tt := newTestApp(t)
	setupOneBot(a, "saved-api-key")
	apiID := a.prefs.AI.Active
	a.addAIConnection("chatgpt")
	id := a.prefs.AI.Active
	a.chatgpt.sessions[id] = chatgpt.Session{Status: chatgpt.Connected, Sharing: true, Email: "test@example.invalid"}
	a.chatgpt.loaded[id] = true
	a.prefs.AI.Current().Models = []agent.ModelInfo{{ID: "account-model", Enabled: true}}
	a.prefs.AI.Current().Model = "account-model"
	if s := a.qaSettings(); s.APIKey != "" || len(s.APIKeys) != 0 || s.TokenSource == nil || s.Protocol != agent.ChatGPT {
		t.Fatal("API key leaked into OAuth connection")
	}
	a.showChatGPTWelcome()
	a.openSettings(tabAI)
	tt.SetSize(1200, 900)
	tt.Frame()
	if !tt.HasText("正在使用 ChatGPT 套餐") || !tt.HasText("知道了") {
		t.Fatalf("missing first-use disclosure: %v", tt.Texts())
	}
	if err := tt.Click("知道了"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	p, err := a.prefStore.Load()
	if err != nil || !p.AI.ChatGPTWelcomeSeen {
		t.Fatal("welcome not persisted", err)
	}
	a.selectAIConnection(apiID)
	if a.qaSettings().APIKey != "saved-api-key" {
		t.Fatal("existing provider affected")
	}
	a.selectAIConnection(id)
	if a.qaSettings().APIKey != "" {
		t.Fatal("cross-provider key leak")
	}
}

func TestChatGPTSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set FREEFOLIO_SNAPSHOT to inspect ChatGPT settings")
	for _, mode := range []string{"disconnected", "connected", "welcome", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			a, tt := newTestApp(t)
			a.addAIConnection("chatgpt")
			id := a.prefs.AI.Active
			tt.SetSize(1120, 820)
			if mode != "disconnected" {
				a.chatgpt.sessions[id] = chatgpt.Session{Status: chatgpt.Connected, Sharing: true, Email: "writer@example.invalid"}
				a.prefs.AI.ChatGPTWelcomeSeen = mode != "welcome"
				a.prefs.AI.Current().Models = []agent.ModelInfo{{ID: "account-model", Name: "Account model", Enabled: true}}
				a.prefs.AI.Current().Model = "account-model"
			}
			if mode == "narrow" {
				tt.SetSize(760, 680)
			}
			a.openSettings(tabAI)
			if mode == "welcome" {
				a.showChatGPTWelcome()
			}
			settle(tt)
			savePNG(t, filepath.Join(dir, "chatgpt-"+mode+".png"), tt)
		})
	}
}

func TestChatGPTUsageFailureHasActionableText(t *testing.T) {
	err := &agent.APIError{Status: 429, Code: "subscription_sharing_usage_limit_exceeded", Message: "limit"}
	if got := qaErrorText(err); got == "limit" || agent.Retryable(err) {
		t.Fatal("usage failure uses generic retry")
	}
	if got := qaErrorText(&chatgpt.Error{Code: "sharing_disabled"}); got != "已登录 ChatGPT，但尚未允许使用套餐。请在设置中授权。" {
		t.Fatalf("permission guidance: %s", got)
	}
}

func TestChatGPTRefreshCatalogRemovesUnavailableModels(t *testing.T) {
	a, _ := newTestApp(t)
	a.addAIConnection("chatgpt")
	id := a.prefs.AI.Active
	a.chatgpt.sessions[id] = chatgpt.Session{Status: chatgpt.Connected, Sharing: true}
	c := a.prefs.AI.Current()
	c.Models = []agent.ModelInfo{{ID: "removed", Enabled: true}}
	c.Model = "removed"
	a.qa.listModels = func(agent.Settings) ([]agent.ModelInfo, error) {
		return []agent.ModelInfo{{ID: "z", Name: "First"}, {ID: "a", Name: "Second"}}, nil
	}
	a.fetchModels()
	if len(c.Models) != 2 || c.Models[0].ID != "z" || c.Model != "" {
		t.Fatalf("stale entitlement: %+v", c)
	}
}

func TestChatGPTNewAccountsHaveDistinctDefaultLabels(t *testing.T) {
	a, _ := newTestApp(t)
	a.addAIConnection("chatgpt")
	first := a.prefs.AI.Current().Name
	a.addAIConnection("chatgpt")
	if a.prefs.AI.Current().Name == first {
		t.Fatal("account picker has duplicate default labels")
	}
}
