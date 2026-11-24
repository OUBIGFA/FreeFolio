// Package chatgpt implements the public Sign in with ChatGPT protocol for a
// local FreeFolio installation. It contains no DevKit code or UI dependencies.
package chatgpt

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"
)

const (
	Connected      = "connected"
	Disconnected   = "disconnected"
	ReauthRequired = "reauth_required"
	UsageURL       = "https://chatgpt.com/settings/usage"
	resource       = "https://api.openai.com/v1"
	scopes         = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
	sharingScope   = "chatgpt.tokens.use.direct"
)

// Session contains display data only. Credentials never leave this package
// except for a short-lived access token requested by the HTTP adapter.
type Session struct {
	Status      string
	Email, Name string
	Sharing     bool
	Pending     bool
}

type tokenSet struct {
	Access   string    `json:"access,omitempty"`
	Refresh  string    `json:"refresh,omitempty"`
	ID       string    `json:"id,omitempty"`
	Expires  time.Time `json:"expires"`
	Earliest time.Time `json:"earliest,omitempty"`
	Scopes   []string  `json:"scopes"`
}

type pendingTokens struct {
	Tokens   tokenSet  `json:"tokens"`
	Received time.Time `json:"received"`
	Nonce    string    `json:"nonce,omitempty"`
}

type profile struct {
	ClientID string         `json:"clientID"`
	Subject  string         `json:"subject,omitempty"`
	Email    string         `json:"email,omitempty"`
	Name     string         `json:"name,omitempty"`
	Status   string         `json:"status"`
	Tokens   *tokenSet      `json:"tokens,omitempty"`
	Pending  *pendingTokens `json:"pending,omitempty"`
}

func (p profile) session() Session {
	status := p.Status
	if status == "" {
		status = Disconnected
	}
	return Session{Status: status, Email: p.Email, Name: p.Name, Pending: p.Pending != nil, Sharing: status == Connected && p.Tokens != nil && slices.Contains(p.Tokens.Scopes, sharingScope)}
}

// Client owns encrypted profiles keyed by the existing OneBot connection ID.
// File locks, rather than instance-local caches, serialize rotating tokens even
// when two FreeFolio processes briefly overlap during startup or an update.
type Client struct {
	dir    string
	issuer string
	http   *http.Client
	crypt  func([]byte, bool) ([]byte, error)
}

func New(credentialsDir string) *Client {
	c := &Client{dir: credentialsDir, issuer: "https://auth.openai.com", http: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.crypt = func(data []byte, seal bool) ([]byte, error) { return protect(credentialsDir, data, seal) }
	return c
}

func (c *Client) Session(ctx context.Context, id string) (Session, error) {
	out := Session{Status: Disconnected}
	err := c.withState(ctx, func(s *savedState) error { out = s.Profiles[id].session(); return nil })
	return out, err
}

// Source binds a request to one connection. Switching the visible provider
// cannot change the account used by an already-started conversation.
func (c *Client) Source(id string) *TokenSource { return &TokenSource{client: c, id: id} }

type TokenSource struct {
	client *Client
	id     string
}

func (source *TokenSource) AccessToken(ctx context.Context) (string, error) {
	c := source.client
	var token string
	err := c.withState(ctx, func(s *savedState) error {
		p, ok := s.Profiles[source.id]
		if !ok {
			return failure("sign_in_required")
		}
		if p.Pending != nil {
			if err := c.acceptPending(ctx, &p); err != nil {
				return c.handleIdentityFailure(s, source.id, p, err)
			}
			s.Profiles[source.id] = p
			if err := c.save(s); err != nil {
				return err
			}
		}
		if p.Status != Connected || p.Tokens == nil {
			return failure("sign_in_required")
		}
		if !slices.Contains(p.Tokens.Scopes, sharingScope) {
			return failure("sharing_disabled")
		}
		if time.Until(p.Tokens.Expires) < time.Minute {
			if time.Now().Before(p.Tokens.Earliest) {
				if !time.Now().Before(p.Tokens.Expires) {
					return failure("refresh_not_ready")
				}
			} else if err := c.refresh(ctx, s, source.id, &p); err != nil {
				return err
			}
		}
		if p.Tokens == nil || !slices.Contains(p.Tokens.Scopes, sharingScope) {
			return failure("sharing_disabled")
		}
		token = p.Tokens.Access
		return nil
	})
	return token, err
}

func (c *Client) handleIdentityFailure(s *savedState, id string, p profile, err error) error {
	if IsCode(err, "invalid_identity") || IsCode(err, "account_mismatch") {
		p.Pending = nil
		p.Tokens = nil
		p.Status = ReauthRequired
		s.Profiles[id] = p
		if saveErr := c.save(s); saveErr != nil {
			return saveErr
		}
	}
	return err
}

// Disconnect preserves the issued registration, but removes every usable and
// pending token. Remote revocation failure is reported after local sign-out.
func (c *Client) Disconnect(ctx context.Context, id string) error {
	var remote error
	err := c.withState(ctx, func(s *savedState) error {
		p, ok := s.Profiles[id]
		if !ok {
			return nil
		}
		token := ""
		if p.Tokens != nil {
			token = p.Tokens.Refresh
		}
		if p.Pending != nil {
			token = p.Pending.Tokens.Refresh
		}
		if token != "" {
			remote = c.revoke(ctx, p.ClientID, token)
		}
		p.Tokens = nil
		p.Pending = nil
		p.Status = Disconnected
		s.Profiles[id] = p
		return c.save(s)
	})
	if err != nil {
		return err
	}
	return remote
}

// Forget is only used after disconnecting a removed OneBot connection.
func (c *Client) Forget(ctx context.Context, id string) error {
	return c.withState(ctx, func(s *savedState) error {
		p := s.Profiles[id]
		if p.Tokens != nil || p.Pending != nil {
			return errors.New("disconnect the ChatGPT account before removing it")
		}
		delete(s.Profiles, id)
		return c.save(s)
	})
}
