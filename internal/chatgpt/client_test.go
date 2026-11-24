package chatgpt

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Tests run real callback listeners, signed identity verification and encrypted
// storage. Only the OS secret store and the external identity server are local.
type fixture struct {
	c                     *Client
	server                *httptest.Server
	key                   *rsa.PrivateKey
	nonce, subject, scope string
	refreshes             atomic.Int32
	keyDown, revokeDown   atomic.Bool
	badNonce              bool
	authorizations        []url.Values
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: key, subject: "subject-a", scope: scopes}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	f.c = New(filepath.Join(t.TempDir(), "credentials"))
	f.c.issuer = f.server.URL
	block, _ := aes.NewCipher(bytes.Repeat([]byte{9}, 32))
	aead, _ := cipher.NewGCM(block)
	f.c.crypt = func(data []byte, encrypt bool) ([]byte, error) {
		if !encrypt {
			if len(data) < aead.NonceSize() {
				return nil, errors.New("bad ciphertext")
			}
			return aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
		}
		nonce := make([]byte, aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
		return aead.Seal(nonce, nonce, data, nil), nil
	}
	return f
}

func (f *fixture) jwt(client string) string {
	nonce := f.nonce
	if f.badNonce {
		nonce = "wrong"
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
	claims, _ := json.Marshal(map[string]any{"iss": f.server.URL, "aud": client, "sub": f.subject, "nonce": nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "email": "test@example.invalid"})
	input := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	hash := sha256.Sum256([]byte(input))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, hash[:])
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		json.NewEncoder(w).Encode(map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/jwks", "revocation_endpoint": f.server.URL + "/revoke"})
	case "/jwks":
		if f.keyDown.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":"unavailable"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(f.key.E)).Bytes())}}})
	case "/token":
		r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			f.refreshes.Add(1)
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-access", "refresh_token": fmt.Sprintf("synthetic-refresh-%d", f.refreshes.Load()), "id_token": f.jwt(r.Form.Get("client_id")), "token_type": "Bearer", "scope": f.scope, "expires_in": 3600})
	case "/revoke":
		if f.revokeDown.Load() {
			w.WriteHeader(503)
		}
	default:
		w.WriteHeader(404)
	}
}

func (f *fixture) browser(raw string) error {
	u, _ := url.Parse(raw)
	q := u.Query()
	f.authorizations = append(f.authorizations, q)
	f.nonce = q.Get("nonce")
	callback, _ := url.Parse(q.Get("redirect_uri"))
	v := url.Values{"state": {q.Get("state")}, "code": {"one-use-code"}}
	if q.Get("client_id") == "dynamic_agent_client" {
		v.Set("client_id", "oaiapp_test")
	}
	callback.RawQuery = v.Encode()
	res, err := http.Get(callback.String())
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	return nil
}

func TestSignInEncryptedRestartAndReauthorization(t *testing.T) {
	f := newFixture(t)
	s, err := f.c.SignIn(context.Background(), "one", false, f.browser)
	if err != nil || s.Status != Connected || !s.Sharing || s.Email != "test@example.invalid" {
		t.Fatalf("sign in: %+v %v", s, err)
	}
	q := f.authorizations[0]
	if q.Get("agent_name_hint") != "FreeFolio" || !strings.HasPrefix(q.Get("ext_agent_host_id"), "urn:uuid:") || q.Get("code_challenge_method") != "S256" || q.Get("scope") != scopes {
		t.Fatal("incomplete authorization")
	}
	data, err := os.ReadFile(filepath.Join(f.c.dir, storeName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("synthetic")) || bytes.Contains(data, []byte("test@example")) {
		t.Fatal("plaintext credential leak")
	}
	restarted := New(f.c.dir)
	restarted.issuer = f.c.issuer
	restarted.crypt = f.c.crypt
	if token, err := restarted.Source("one").AccessToken(context.Background()); err != nil || token != "synthetic-access" {
		t.Fatal("restart", err)
	}
	if _, err = f.c.SignIn(context.Background(), "one", false, f.browser); err != nil {
		t.Fatal(err)
	}
	q2 := f.authorizations[1]
	if q2.Get("client_id") != "oaiapp_test" || q2.Get("agent_name_hint") != "" || q2.Get("ext_agent_host_id") != q.Get("ext_agent_host_id") || q2.Get("state") == q.Get("state") {
		t.Fatal("registration not reused")
	}
}

func TestSignInRejectsWrongIdentityAndMissingConsent(t *testing.T) {
	f := newFixture(t)
	f.badNonce = true
	if _, err := f.c.SignIn(context.Background(), "one", false, f.browser); err == nil {
		t.Fatal("accepted wrong nonce")
	}
	f.badNonce = false
	f.scope = "openid profile email"
	s, err := f.c.SignIn(context.Background(), "one", false, f.browser)
	if err != nil || s.Status != Connected || s.Sharing {
		t.Fatalf("identity-only login: %+v %v", s, err)
	}
	if _, err = f.c.Source("one").AccessToken(context.Background()); !IsCode(err, "sharing_disabled") {
		t.Fatal("inference without consent", err)
	}
	f.subject = "other"
	if _, err = f.c.SignIn(context.Background(), "one", true, f.browser); !IsCode(err, "account_mismatch") {
		t.Fatal("replaced account", err)
	}
}

func TestCallbackWrongStateDoesNotConsumeAttemptAndCancellationClosesListener(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var callback string
	_, err := f.c.SignIn(ctx, "one", false, func(raw string) error {
		u, _ := url.Parse(raw)
		cb, _ := url.Parse(u.Query().Get("redirect_uri"))
		cb.RawQuery = "state=wrong&code=wrong&client_id=wrong"
		callback = cb.String()
		res, e := http.Get(callback)
		if e != nil {
			return e
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Errorf("wrong state: %d", res.StatusCode)
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled login", err)
	}
	if res, err := http.Get(callback); err == nil {
		res.Body.Close()
		t.Fatal("callback remained open")
	}
	s, err := f.c.Session(context.Background(), "one")
	if err != nil || s.Status != Disconnected {
		t.Fatal("cancelled state", err)
	}
}

func TestRefreshCheckpointSurvivesSigningServiceOutageAndSerializes(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.SignIn(context.Background(), "one", false, f.browser); err != nil {
		t.Fatal(err)
	}
	err := f.c.withState(context.Background(), func(s *savedState) error {
		p := s.Profiles["one"]
		p.Tokens.Expires = time.Now().Add(-time.Minute)
		s.Profiles["one"] = p
		return f.c.save(s)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.keyDown.Store(true)
	if _, err := f.c.Source("one").AccessToken(context.Background()); !IsCode(err, "identity_unavailable") {
		t.Fatal("expected retryable key error", err)
	}
	if f.refreshes.Load() != 1 {
		t.Fatal("wrong refresh count")
	}
	f.keyDown.Store(false)
	restarted := New(f.c.dir)
	restarted.issuer = f.c.issuer
	restarted.crypt = f.c.crypt
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := restarted.Source("one").AccessToken(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.refreshes.Load() != 1 {
		t.Fatal("consumed rotated token twice")
	}
}

func TestDisconnectKeepsRegistrationAndReportsFailedRevocation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.SignIn(context.Background(), "one", false, f.browser); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.SignIn(context.Background(), "two", false, f.browser); err != nil {
		t.Fatal(err)
	}
	f.revokeDown.Store(true)
	if err := f.c.Disconnect(context.Background(), "one"); !IsCode(err, "revocation_unconfirmed") {
		t.Fatal("missing revoke warning", err)
	}
	s, err := f.c.Session(context.Background(), "one")
	if err != nil || s.Status != Disconnected {
		t.Fatal("not signed out", err)
	}
	if _, err := f.c.Source("two").AccessToken(context.Background()); err != nil {
		t.Fatal("other account affected", err)
	}
	if _, err := f.c.SignIn(context.Background(), "one", false, f.browser); err != nil {
		t.Fatal(err)
	}
	if f.authorizations[2].Get("client_id") != "oaiapp_test" {
		t.Fatal("logout discarded registration")
	}
}

func TestDamagedEncryptedStateIsNeverOverwritten(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.SignIn(context.Background(), "one", false, f.browser); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.c.dir, storeName)
	data := []byte("damaged state")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.SignIn(context.Background(), "two", false, f.browser); err == nil {
		t.Fatal("overwrote broken state")
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Fatal("lost original state")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCancelledIdentityVerificationCannotResumeAsLogin(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/jwks" {
			cancel()
			return nil, context.Canceled
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	if _, err := f.c.SignIn(ctx, "one", false, f.browser); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	s, err := f.c.Session(context.Background(), "one")
	if err != nil || s.Pending || s.Status == Connected {
		t.Fatalf("cancel left resumable login: %+v %v", s, err)
	}
}

func TestTokenRequestsRejectRedirects(t *testing.T) {
	f := newFixture(t)
	var leaked atomic.Bool
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer trap.Close()
	f.c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			return &http.Response{StatusCode: 307, Header: http.Header{"Location": {trap.URL}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	if _, err := f.c.SignIn(context.Background(), "one", false, f.browser); err == nil || leaked.Load() {
		t.Fatal("redirected credential exchange")
	}
}

func TestOAuthErrorsRedactSubmittedSecrets(t *testing.T) {
	f := newFixture(t)
	f.c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: http.Header{"X-Request-Id": {"synthetic-refresh"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"synthetic-refresh","param":"synthetic-refresh"}}`)), Request: r}, nil
	})
	err := f.c.jsonRequest(context.Background(), http.MethodPost, f.server.URL+"/token", url.Values{"refresh_token": {"synthetic-refresh"}}, nil)
	if err == nil {
		t.Fatal("missing error")
	}
	raw, _ := json.Marshal(err)
	if strings.Contains(string(raw), "synthetic-refresh") {
		t.Fatal("credential in OAuth diagnostics")
	}
}
