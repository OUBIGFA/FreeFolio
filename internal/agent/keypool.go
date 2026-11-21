package agent

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
)

// Key strategies of a connection with several API keys, as API gateways
// such as LiteLLM and new-api offer them: spread requests over the keys in
// turn or at random, or keep to the first key and fall back to the next
// only when it fails.
const (
	KeyRoundRobin = ""
	KeyRandom     = "random"
	KeyFailover   = "failover"
)

// keyTurns counts each connection's requests for round robin, by
// connection ID. It lives for the process: rotation needs no persistence.
var keyTurns sync.Map

// keyOrder is the order a request tries the keys in: the strategy's pick
// first, then the rest, so a failing key can hand over to the others.
func (s Settings) keyOrder() []string {
	n := len(s.APIKeys)
	start := 0
	switch s.KeyStrategy {
	case KeyFailover:
	case KeyRandom:
		start = rand.IntN(n)
	default:
		turn, _ := keyTurns.LoadOrStore(s.ConnectionID, new(atomic.Uint64))
		start = int((turn.(*atomic.Uint64).Add(1) - 1) % uint64(n))
	}
	return append(append([]string(nil), s.APIKeys[start:]...), s.APIKeys[:start]...)
}

// keyPool tries a request with its keys in order. It moves to the next key
// only on a refusal that another key may not get (bad, unpaid or
// rate-limited key, or a server error) and only before any reply text was
// shown, so a partial answer is never repeated.
type keyPool struct {
	settings Settings
	keys     []string
}

func (p *keyPool) Complete(ctx context.Context, req Request, onDelta func(Delta)) (Response, error) {
	streamed := false
	relay := func(d Delta) {
		streamed = true
		if onDelta != nil {
			onDelta(d)
		}
	}
	var resp Response
	var err error
	for i, key := range p.keys {
		s := p.settings
		s.APIKey, s.APIKeys = key, nil
		resp, err = s.wireProvider().Complete(ctx, req, relay)
		if err == nil || streamed || ctx.Err() != nil || i == len(p.keys)-1 || !keyRefused(err) {
			break
		}
	}
	return resp, err
}

// keyRefused reports a refusal that may be the key's own.
func keyRefused(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	switch api.Status {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	}
	return api.Status >= 500
}
