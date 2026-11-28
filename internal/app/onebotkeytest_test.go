package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
	"github.com/egoist/mygo/ui"
)

// keyProbe answers with a key it knows and refuses any other, as a service
// refuses a bad key.
type keyProbe struct {
	key   string
	calls *[]string
}

func (p keyProbe) Complete(_ context.Context, req agent.Request, onDelta func(agent.Delta)) (agent.Response, error) {
	*p.calls = append(*p.calls, p.key)
	if p.key != "good" {
		return agent.Response{}, &agent.APIError{Status: 401, Message: "Incorrect API key provided"}
	}
	onDelta(agent.Delta{Text: "Hi"})
	return agent.Response{Message: agent.Message{Role: agent.Assistant, Text: "Hi"}}, nil
}

func keyTestApp(t *testing.T) (*App, *ui.Tester, *[]string) {
	a, tt := newTestApp(t)
	setupOneBot(a, "good")
	a.qa.keyInput = "bad"
	_ = a.saveAIKey()
	a.qa.sync = true
	var calls []string
	a.qa.provider = func(s agent.Settings) agent.Provider {
		if len(s.APIKeys) > 0 || s.Model != "test-model" || s.Parameters != (agent.Parameters{}) {
			t.Errorf("a key is tested alone, with the model and no parameters: %+v", s)
		}
		return keyProbe{key: s.APIKey, calls: &calls}
	}
	a.openSettings(tabAI)
	tt.Frame()
	if err := tt.Click("供应商与连接"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	return a, tt, &calls
}

// Every key can be tested at once; each shows whether it answered.
func TestOneBotTestsAllKeys(t *testing.T) {
	a, tt, calls := keyTestApp(t)
	if err := tt.Click("测试全部 Key"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*calls) != 2 {
		t.Fatalf("tested %q, want both keys", *calls)
	}
	if r, _ := a.keyTestOf("good"); r.busy || r.err != nil {
		t.Fatalf("good key: %+v", r)
	}
	if r, _ := a.keyTestOf("bad"); r.err == nil {
		t.Fatal("the refused key must fail")
	}
	texts := strings.Join(tt.Texts(), "|")
	if !strings.Contains(texts, "可用 · ") || !strings.Contains(texts, "不可用") || !strings.Contains(texts, "Key 2 测试失败：") {
		t.Fatalf("results not shown: %s", texts)
	}
}

// One key can be tested alone, and a change of model leaves its result.
func TestOneBotTestsOneKey(t *testing.T) {
	a, tt, calls := keyTestApp(t)
	if err := tt.Click("测试 Key 2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*calls) != 1 || (*calls)[0] != "bad" {
		t.Fatalf("tested %q, want only Key 2", *calls)
	}
	if _, ok := a.keyTestOf("good"); ok {
		t.Fatal("Key 1 was not tested")
	}
	c := a.prefs.AI.Current()
	c.Models = append(c.Models, agent.ModelInfo{ID: "other", Enabled: true})
	c.Model = "other"
	if _, ok := a.keyTestOf("bad"); ok {
		t.Fatal("a result must not outlive a change of model")
	}
}

func TestOneBotMediaKeyUsesOnlyImageEndpoint(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Header.Get("Authorization"))
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			http.Error(w, "only images", http.StatusForbidden)
			return
		}
		if calls[len(calls)-1] != "Bearer good" {
			http.Error(w, "invalid key", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"b64_json":"iVBORw0KGgo="}]}`)
	}))
	defer srv.Close()
	a, tt, _ := keyTestApp(t)
	conn := a.prefs.AI.Current()
	conn.BaseURL, conn.Protocol = srv.URL+"/v1", agent.Media
	tt.Frame()
	if err := tt.Click("测试全部 Key 生图（可能计费）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	good, ok := a.keyTestOf("good")
	bad, tested := a.keyTestOf("bad")
	if strings.Join(calls, ",") != "Bearer good,Bearer bad" || !ok || good.err != nil || !tested || bad.err == nil {
		t.Fatalf("calls=%q good=%+v bad=%+v", calls, good, bad)
	}
}

// The reasoning effort is a select of named levels, the default first.
func TestOneBotReasoningSelect(t *testing.T) {
	a, tt, _ := keyTestApp(t)
	if err := tt.Click("生成参数"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("服务默认"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("高"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	tt.Frame()
	if got := a.prefs.AI.Current().Parameters.Reasoning; got != "high" {
		t.Fatalf("reasoning = %q, want high", got)
	}
}
