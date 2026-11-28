package app

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/OUBIGFA/FreeFolio/internal/agent"
)

// keyTest is the last test of one API key of the current provider.
type keyTest struct {
	busy bool
	err  error
	took time.Duration
	gen  uint64
}

// keyTestTimeout bounds one key's test.
const keyTestTimeout = 30 * time.Second

// testAIKeys asks the current model one short question with each of keys
// alone, all at once; "" tests a service that needs no key. A key answers
// as soon as the reply starts, which ends the request.
func (ob *qaState) testAIKeys(keys ...string) {
	c := ob.host.preferences().AI.Current()
	if c == nil || c.Protocol == agent.ChatGPT {
		return
	}
	q := ob
	base := ob.qaSettings()
	sig := base.Signature()
	if q.keyTestFor != sig {
		q.keyTestFor, q.keyTests = sig, nil
	}
	if q.keyTests == nil {
		q.keyTests = map[string]keyTest{}
	}
	for _, key := range keys {
		if q.keyTests[key].busy {
			continue
		}
		q.keyTestGen++
		gen := q.keyTestGen
		q.keyTests[key] = keyTest{busy: true, gen: gen}
		s := base
		s.APIKey, s.APIKeys, s.Parameters = key, nil, agent.Parameters{}
		var provider agent.Provider
		if s.Protocol != agent.Media && s.Check() == nil {
			provider = s.NewProvider()
			if q.provider != nil {
				provider = q.provider(s)
			}
		}
		run := func() (time.Duration, error) {
			if s.Protocol == agent.Media {
				start := time.Now()
				if !agent.SupportsMedia(s.Protocol, s.Vendor, s.Capabilities, agent.MediaImage) {
					ctx, cancel := context.WithTimeout(context.Background(), keyTestTimeout)
					defer cancel()
					_, err := s.DiscoverModels(ctx)
					return time.Since(start), err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				_, err := s.GenerateMedia(ctx, agent.MediaRequest{Kind: agent.MediaImage, Prompt: "A blue circle on a white background", Count: 1}, nil)
				return time.Since(start), err
			}
			if err := s.Check(); err != nil {
				return 0, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), keyTestTimeout)
			defer cancel()
			var answered atomic.Bool
			start := time.Now()
			_, err := provider.Complete(ctx, agent.Request{Messages: []agent.Message{{Role: agent.User, Text: "Hi"}}}, func(agent.Delta) {
				answered.Store(true)
				cancel()
			})
			if answered.Load() {
				err = nil
			}
			return time.Since(start), err
		}
		done := func(took time.Duration, err error) {
			if q.keyTestFor != sig || q.keyTests[key].gen != gen {
				return
			}
			q.keyTests[key] = keyTest{err: err, took: took, gen: gen}
		}
		if q.sync || ob.host.window() == nil {
			done(run())
			continue
		}
		go func() { took, err := run(); ob.host.window().Update(func() { done(took, err) }) }()
	}
}

// keyTestsCurrent are the key tests made with the provider as it is set
// now: a change of address or model leaves the earlier ones behind.
func (ob *qaState) keyTestsCurrent() map[string]keyTest {
	if ob.host.preferences().AI.Current() == nil || ob.keyTestFor != ob.qaSettings().Signature() {
		return nil
	}
	return ob.keyTests
}

// keyTestsBusy reports a test still running for the current provider.
func (ob *qaState) keyTestsBusy() bool {
	for _, r := range ob.keyTestsCurrent() {
		if r.busy {
			return true
		}
	}
	return false
}

// keyTestOf is the last test of key for the current provider.
func (ob *qaState) keyTestOf(key string) (keyTest, bool) {
	r, ok := ob.keyTestsCurrent()[key]
	return r, ok
}
