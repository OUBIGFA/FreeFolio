package gitsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubDeviceLoginRefreshAndEncryptedStore(t *testing.T) {
	polls := 0
	refreshes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/device/code":
			json.NewEncoder(w).Encode(map[string]any{"device_code": "private-device", "user_code": "TEST-CODE", "verification_uri": "https://github.com/login/device", "expires_in": 60, "interval": 0})
		case "/login/oauth/access_token":
			r.ParseForm()
			if r.Form.Get("client_secret") != "" {
				t.Error("client secret sent")
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				refreshes++
				json.NewEncoder(w).Encode(map[string]any{"access_token": "refreshed-secret", "refresh_token": "next-refresh", "expires_in": 3600, "refresh_token_expires_in": 9000})
				return
			}
			polls++
			json.NewEncoder(w).Encode(map[string]any{"access_token": "original-secret", "refresh_token": "refresh-secret", "expires_in": 1, "refresh_token_expires_in": 9000})
		case "/user":
			json.NewEncoder(w).Encode(map[string]any{"id": 42, "login": "octo", "name": "Octo"})
		case "/user/emails":
			json.NewEncoder(w).Encode([]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := NewGitHub(t.TempDir(), "Iv1.fixture")
	c.web = server.URL
	c.api = server.URL
	c.client = server.Client()
	d, e := c.Begin(context.Background())
	if e != nil || d.Code != "TEST-CODE" {
		t.Fatal(d, e)
	}
	a, e := c.Complete(context.Background(), d)
	if e != nil || a.Login != "octo" {
		t.Fatal(a, e)
	}
	token, e := c.Token(context.Background(), a.ID)
	if e != nil || token != "refreshed-secret" || refreshes != 1 || polls != 1 {
		t.Fatal(token, e, refreshes, polls)
	}
	data, e := os.ReadFile(filepath.Join(c.dir, "github-accounts"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "octo") {
		t.Fatal("credentials stored as plaintext")
	}
	accounts, e := c.Accounts()
	if e != nil || len(accounts) != 1 {
		t.Fatal(accounts, e)
	}
	if e = c.Disconnect(a.ID); e != nil {
		t.Fatal(e)
	}
	accounts, e = c.Accounts()
	if e != nil || len(accounts) != 0 {
		t.Fatal(accounts, e)
	}
}

func TestGitHubMissingRegistrationAndUnsafeHelper(t *testing.T) {
	c := NewGitHub(t.TempDir(), "")
	if _, e := c.Begin(context.Background()); e == nil {
		t.Fatal("missing registration accepted")
	}
	for _, query := range []string{"protocol=https\nhost=evil.test\n\n", "protocol=http\nhost=github.com\n\n", "protocol=https\nhost=github.com\npath=other/repo.git\n\n"} {
		if CredentialAllowed(query, "owner/repo") {
			t.Fatal("credential leaked", query)
		}
	}
	if !CredentialAllowed("protocol=https\nhost=github.com\npath=owner/repo.git\n\n", "owner/repo") {
		t.Fatal("valid credential request rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Complete(ctx, Device{Expires: time.Now().Add(time.Minute)}); e == nil {
		t.Fatal("cancel ignored")
	}
}

func TestCredentialSessionScopesTokenAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env, close, e := CredentialSession(ctx, "owner/repo", "private-token")
	if e != nil {
		t.Fatal(e)
	}
	defer close()
	for _, entry := range env {
		if strings.Contains(entry, "private-token") {
			t.Fatal("token in environment")
		}
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "FREEFOLIO_") {
			t.Setenv(key, value)
		}
	}
	var out strings.Builder
	if e = CredentialHelper("get", strings.NewReader("protocol=https\nhost=github.com\npath=owner/repo.git\n\n"), &out); e != nil || !strings.Contains(out.String(), "password=private-token") {
		t.Fatal(out.String(), e)
	}
	out.Reset()
	if e = CredentialHelper("get", strings.NewReader("protocol=https\nhost=evil.test\npath=owner/repo.git\n\n"), &out); e == nil || out.Len() > 0 {
		t.Fatal("out-of-scope token disclosed")
	}
}
