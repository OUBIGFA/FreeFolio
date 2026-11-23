package chatgpt

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func (c *Client) jsonRequest(ctx context.Context, method, endpoint string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return failure("invalid_discovery")
	}
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure("network_error")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return failure("network_error")
	}
	if len(data) > 1<<20 {
		return failure("invalid_response")
	}
	if res.StatusCode != 200 {
		e := &Error{Status: res.StatusCode, Code: "request_failed", RequestID: res.Header.Get("x-request-id"), Retryable: res.StatusCode >= 500, Shape: "text"}
		var b map[string]json.RawMessage
		if json.Unmarshal(data, &b) == nil {
			var code string
			if json.Unmarshal(b["error"], &code) == nil && clientIDPattern.MatchString(code) {
				e.Code = code
				e.Shape = "error_string"
			} else {
				var detail struct {
					Code  string `json:"code"`
					Param string `json:"param"`
				}
				if json.Unmarshal(b["error"], &detail) == nil && detail.Code != "" {
					e.Code = detail.Code
					e.Param = detail.Param
					e.Shape = "error"
				} else if _, ok := b["detail"]; ok {
					e.Shape = "detail"
				}
			}
		}
		for _, field := range []*string{&e.Code, &e.Param, &e.RequestID} {
			for _, name := range []string{"refresh_token", "token", "code", "code_verifier"} {
				if secret := form.Get(name); secret != "" {
					*field = strings.ReplaceAll(*field, secret, "[redacted]")
				}
			}
			if len(*field) > 200 {
				*field = (*field)[:200]
			}
		}
		return e
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		return failure("invalid_response")
	}
	return nil
}

func (c *Client) exchange(ctx context.Context, endpoint string, form url.Values, prior *tokenSet) (tokenSet, error) {
	var r struct {
		Access   string          `json:"access_token"`
		Refresh  string          `json:"refresh_token"`
		ID       string          `json:"id_token"`
		Type     string          `json:"token_type"`
		Expires  float64         `json:"expires_in"`
		Scope    *string         `json:"scope"`
		Earliest json.RawMessage `json:"earliest_refresh_at"`
	}
	if err := c.jsonRequest(ctx, http.MethodPost, endpoint, form, &r); err != nil {
		return tokenSet{}, err
	}
	if r.Scope == nil && prior == nil {
		return tokenSet{}, failure("invalid_tokens")
	}
	t := tokenSet{Access: r.Access, Refresh: r.Refresh, ID: r.ID}
	if r.Scope != nil {
		t.Scopes = strings.Fields(*r.Scope)
	} else {
		t.Scopes = slices.Clone(prior.Scopes)
	}
	needsAccess := prior != nil || slices.Contains(t.Scopes, sharingScope) || slices.Contains(t.Scopes, "offline_access")
	if r.Access != "" {
		if !strings.EqualFold(r.Type, "Bearer") || r.Expires <= 0 || r.Expires > 86400*365 || math.IsNaN(r.Expires) || math.IsInf(r.Expires, 0) {
			return tokenSet{}, failure("invalid_tokens")
		}
		t.Expires = time.Now().Add(time.Duration(r.Expires * float64(time.Second)))
	} else if needsAccess {
		return tokenSet{}, failure("invalid_tokens")
	}
	if (prior != nil || slices.Contains(t.Scopes, "offline_access")) && r.Refresh == "" {
		return tokenSet{}, failure("invalid_tokens")
	}
	if len(r.Earliest) > 0 && string(r.Earliest) != "null" {
		var raw string
		if json.Unmarshal(r.Earliest, &raw) != nil {
			raw = string(r.Earliest)
		}
		if number, err := strconv.ParseFloat(raw, 64); err == nil {
			if number > 1e12 {
				number /= 1000
			}
			if math.IsInf(number, 0) || math.IsNaN(number) || number < 0 || number > 1e11 {
				return tokenSet{}, failure("invalid_tokens")
			}
			t.Earliest = time.Unix(int64(number), 0)
		} else {
			var err error
			t.Earliest, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				return tokenSet{}, failure("invalid_tokens")
			}
		}
	}
	return t, nil
}

func (c *Client) acceptPending(ctx context.Context, p *profile) error {
	rotation := p.Pending
	if rotation == nil {
		return nil
	}
	d, err := c.discover(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure("identity_unavailable")
	}
	var jwks jose.JSONWebKeySet
	if err := c.jsonRequest(ctx, http.MethodGet, d.Keys, nil, &jwks); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure("identity_unavailable")
	}
	keys := []crypto.PublicKey{}
	for _, k := range jwks.Keys {
		if k.Valid() && k.IsPublic() && (k.Use == "" || k.Use == "sig") && (k.Algorithm == "" || k.Algorithm == "RS256") {
			keys = append(keys, k.Key)
		}
	}
	if len(keys) == 0 {
		return failure("identity_unavailable")
	}
	verifier := oidc.NewVerifier(c.issuer, &oidc.StaticKeySet{PublicKeys: keys}, &oidc.Config{ClientID: p.ClientID, SupportedSigningAlgs: []string{"RS256"}, Now: func() time.Time { return rotation.Received }})
	identity, err := verifier.Verify(ctx, rotation.Tokens.ID)
	if err != nil {
		return failure("invalid_identity")
	}
	var claims struct {
		Email           string `json:"email"`
		Name            string `json:"name"`
		AuthorizedParty string `json:"azp"`
		Issued          *int64 `json:"iat"`
	}
	if identity.Claims(&claims) != nil || identity.Subject == "" || claims.Issued == nil || *claims.Issued > rotation.Received.Add(5*time.Second).Unix() || identity.Expiry.IsZero() || rotation.Nonce != "" && identity.Nonce != rotation.Nonce || claims.AuthorizedParty != "" && claims.AuthorizedParty != p.ClientID || len(identity.Audience) > 1 && claims.AuthorizedParty != p.ClientID {
		return failure("invalid_identity")
	}
	if p.Subject != "" && p.Subject != identity.Subject {
		return failure("account_mismatch")
	}
	p.Subject = identity.Subject
	p.Email = claims.Email
	p.Name = claims.Name
	p.Tokens = &rotation.Tokens
	p.Pending = nil
	p.Status = Connected
	return nil
}

func (c *Client) refresh(ctx context.Context, s *savedState, id string, p *profile) error {
	if p.Tokens.Refresh == "" {
		p.Status = ReauthRequired
		p.Tokens = nil
		s.Profiles[id] = *p
		if err := c.save(s); err != nil {
			return err
		}
		return failure("sign_in_required")
	}
	d, err := c.discover(ctx)
	if err != nil {
		return err
	}
	tokens, err := c.exchange(ctx, d.Token, url.Values{"grant_type": {"refresh_token"}, "client_id": {p.ClientID}, "refresh_token": {p.Tokens.Refresh}, "resource": {resource}}, p.Tokens)
	if err != nil {
		if terminalRefresh(err) {
			p.Tokens = nil
			p.Pending = nil
			p.Status = ReauthRequired
			s.Profiles[id] = *p
			if e := c.save(s); e != nil {
				return e
			}
		}
		return err
	}
	if tokens.ID != "" {
		p.Pending = &pendingTokens{Tokens: tokens, Received: time.Now()}
		s.Profiles[id] = *p
		if err := c.save(s); err != nil {
			return err
		}
		if err := c.acceptPending(ctx, p); err != nil {
			return c.handleIdentityFailure(s, id, *p, err)
		}
	} else {
		tokens.ID = p.Tokens.ID
		p.Tokens = &tokens
	}
	s.Profiles[id] = *p
	return c.save(s)
}

func (c *Client) revoke(ctx context.Context, client, token string) error {
	d, err := c.discover(ctx)
	if err != nil || d.Revocation == "" {
		return failure("revocation_unconfirmed")
	}
	for attempt := 0; attempt < 2; attempt++ {
		err = c.jsonRequest(ctx, http.MethodPost, d.Revocation, url.Values{"token": {token}, "token_type_hint": {"refresh_token"}, "client_id": {client}}, nil)
		if err == nil {
			return nil
		}
		var remote *Error
		if !errors.As(err, &remote) || !remote.Retryable || attempt == 1 {
			break
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failure("revocation_unconfirmed")
		case <-timer.C:
		}
	}
	return failure("revocation_unconfirmed")
}
