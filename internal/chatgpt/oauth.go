package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func validClientID(id string) bool {
	return id != "dynamic_agent_client" && clientIDPattern.MatchString(id)
}
func randomValue() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:]), err
}

type discovery struct {
	Issuer        string `json:"issuer"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	Keys          string `json:"jwks_uri"`
	Revocation    string `json:"revocation_endpoint"`
}

func (c *Client) discover(ctx context.Context) (discovery, error) {
	var d discovery
	if err := c.jsonRequest(ctx, http.MethodGet, c.issuer+"/.well-known/openid-configuration", nil, &d); err != nil {
		return d, err
	}
	if d.Issuer != c.issuer {
		return d, failure("invalid_discovery")
	}
	for i, endpoint := range []string{d.Authorization, d.Token, d.Keys, d.Revocation} {
		if endpoint == "" && i == 3 {
			continue
		}
		u, err := url.Parse(endpoint)
		if err != nil || u.User != nil || u.Fragment != "" || u.Scheme+"://"+u.Host != c.issuer {
			return d, failure("invalid_discovery")
		}
	}
	return d, nil
}

type callback struct {
	code, client string
	err          error
}

// SignIn performs one cancellable browser authorization. Opening the browser
// is supplied by the native UI. Only validated credentials become active.
func (c *Client) SignIn(ctx context.Context, id string, reconsent bool, openBrowser func(string) error) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if id == "" || openBrowser == nil {
		return Session{}, failure("invalid_connection")
	}
	var original profile
	var host string
	err := c.withState(ctx, func(s *savedState) error {
		original = s.Profiles[id]
		// Persist the installation identity before browser registration begins.
		if s.HostID == "" {
			if err := c.save(s); err != nil {
				return err
			}
		}
		host = s.HostID
		return nil
	})
	if err != nil {
		return Session{}, err
	}
	d, err := c.discover(ctx)
	if err != nil {
		return Session{}, err
	}
	state, err := randomValue()
	if err != nil {
		return Session{}, err
	}
	nonce, err := randomValue()
	if err != nil {
		return Session{}, err
	}
	verifier, err := randomValue()
	if err != nil {
		return Session{}, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return Session{}, failure("callback_unavailable")
	}
	redirect := "http://" + listener.Addr().String() + "/auth/callback"
	result := make(chan callback, 1)
	var once sync.Once
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		if r.Method != http.MethodGet || r.Host != listener.Addr().String() || r.URL.Path != "/auth/callback" || r.URL.IsAbs() {
			http.NotFound(w, r)
			return
		}
		q, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid sign-in state.", 400)
			return
		}
		answer := callback{code: q.Get("code"), client: q.Get("client_id")}
		if answer.client == "" {
			answer.client = original.ClientID
		}
		if q.Get("error") != "" {
			answer.err = failure("authorization_denied")
		} else if len(q["code"]) != 1 || answer.code == "" || len(q["client_id"]) > 1 || !validClientID(answer.client) || original.ClientID != "" && answer.client != original.ClientID {
			answer.err = failure("registration_incomplete")
		}
		accepted := false
		once.Do(func() { accepted = true; result <- answer })
		if !accepted {
			http.Error(w, "Sign-in already received.", 409)
			return
		}
		// Redirect removes the authorization code from the visible URL without
		// depending on scripts or exposing any returned value in page content.
		w.Header().Set("Location", "/auth/complete")
		w.WriteHeader(http.StatusSeeOther)
	})
	baseHandler := server.Handler
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.Host == listener.Addr().String() && r.URL.Path == "/auth/complete" {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("Return to FreeFolio. The app is verifying your ChatGPT connection. You can close this tab."))
			return
		}
		baseHandler.ServeHTTP(w, r)
	})
	defer server.Close()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			once.Do(func() { result <- callback{err: failure("callback_unavailable")} })
		}
	}()
	clientID := original.ClientID
	if clientID == "" {
		clientID = "dynamic_agent_client"
	}
	hash := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirect}, "scope": {scopes}, "resource": {resource}, "state": {state}, "nonce": {nonce}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "ext_agent_host_id": {host}}
	if original.ClientID == "" {
		q.Set("agent_name_hint", "FreeFolio")
	}
	// Email is sufficient to preselect a returning account. Retained ID tokens
	// stay out of browser process arguments and the address/history UI.
	if original.Email != "" {
		q.Set("login_hint", original.Email)
	}
	if reconsent {
		q.Set("prompt", "consent")
	}
	u, _ := url.Parse(d.Authorization)
	u.RawQuery = q.Encode()
	if err := openBrowser(u.String()); err != nil {
		return Session{}, failure("browser_unavailable")
	}
	var received callback
	select {
	case <-ctx.Done():
		return Session{}, ctx.Err()
	case received = <-result:
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if received.err != nil {
		return Session{}, received.err
	}
	var out Session
	err = c.withState(ctx, func(s *savedState) (resultErr error) {
		p := s.Profiles[id]
		// An account removed/replaced while its browser was open cannot be
		// resurrected by a late callback from the old registration.
		before, _ := json.Marshal(original)
		now, _ := json.Marshal(p)
		if string(before) != string(now) {
			return failure("connection_changed")
		}
		p.ClientID = received.client
		if p.Status == "" {
			p.Status = Disconnected
		}
		s.Profiles[id] = p
		// A failed one-use code exchange must not register a second client.
		if err := c.save(s); err != nil {
			return err
		}
		fields, err := c.exchange(ctx, d.Token, url.Values{"grant_type": {"authorization_code"}, "client_id": {p.ClientID}, "code": {received.code}, "code_verifier": {verifier}, "redirect_uri": {redirect}, "resource": {resource}}, nil)
		if err != nil {
			return err
		}
		if fields.ID == "" {
			return failure("invalid_identity")
		}
		p.Pending = &pendingTokens{Tokens: fields, Received: time.Now(), Nonce: nonce}
		// A cancelled login is different from a temporary signing-service
		// outage: it must never resume as an authenticated session on restart.
		defer func() {
			if ctx.Err() != nil {
				restored := original
				restored.ClientID = received.client
				if restored.Status == "" {
					restored.Status = Disconnected
				}
				s.Profiles[id] = restored
				if err := c.save(s); err != nil {
					resultErr = err
				} else {
					resultErr = ctx.Err()
				}
			}
		}()
		s.Profiles[id] = p
		if err := c.save(s); err != nil {
			return err
		}
		if err := c.acceptPending(ctx, &p); err != nil {
			// A failed reauthorization must preserve a previously usable account.
			if IsCode(err, "invalid_identity") || IsCode(err, "account_mismatch") {
				p.Pending = nil
				s.Profiles[id] = p
				if e := c.save(s); e != nil {
					return e
				}
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		s.Profiles[id] = p
		if err := c.save(s); err != nil {
			return err
		}
		out = p.session()
		return nil
	})
	return out, err
}
