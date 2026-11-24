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
func (a *App) testAIKeys(keys ...string) {
	c := a.prefs.AI.Current()
	if c == nil || c.Protocol == agent.ChatGPT {
		return
	}
	q := &a.qa
	base := a.qaSettings()
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
		if s.Check() == nil {
			provider = s.NewProvider()
			if q.provider != nil {
				provider = q.provider(s)
			}
		}
		run := func() (time.Duration, error) {
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
		if q.sync || a.win == nil {
			done(run())
			continue
		}
		go func() { took, err := run(); a.win.Update(func() { done(took, err) }) }()
	}
}

// keyTestsCurrent are the key tests made with the provider as it is set
// now: a change of address or model leaves the earlier ones behind.
func (a *App) keyTestsCurrent() map[string]keyTest {
	if a.prefs.AI.Current() == nil || a.qa.keyTestFor != a.qaSettings().Signature() {
		return nil
	}
	return a.qa.keyTests
}

// keyTestsBusy reports a test still running for the current provider.
func (a *App) keyTestsBusy() bool {
	for _, r := range a.keyTestsCurrent() {
		if r.busy {
			return true
		}
	}
	return false
}

// keyTestOf is the last test of key for the current provider.
func (a *App) keyTestOf(key string) (keyTest, bool) {
	r, ok := a.keyTestsCurrent()[key]
	return r, ok
}
