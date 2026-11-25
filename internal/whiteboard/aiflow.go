package whiteboard

import (
	"context"
	"errors"
	"sync"
)

// ErrAIUpstream is a node not run because a node it reads failed.
var ErrAIUpstream = errors.New("whiteboard: an upstream node failed")

// RunAIFlow runs the planned nodes, each once every node it reads (deps)
// has finished, at most limit at a time. Independent branches run in
// parallel. A failed node's downstream is reported with ErrAIUpstream and
// not run; a cancelled ctx stops starting nodes and is passed to running
// ones. done is called once per node, from the goroutine that ran it, so
// callers forward it to their own thread. The design follows the
// context-cancelling DAG of Azure/go-workflow (MIT), without its code.
func RunAIFlow(ctx context.Context, order []string, deps map[string][]string, limit int, run func(context.Context, string) error, done func(string, error)) {
	if limit <= 0 {
		limit = 4
	}
	slots := make(chan struct{}, limit)
	finished := make(map[string]chan struct{}, len(order))
	errs := make(map[string]error, len(order))
	var mu sync.Mutex
	for _, id := range order {
		finished[id] = make(chan struct{})
	}
	var wg sync.WaitGroup
	for _, id := range order {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(finished[id])
			result := func(err error) {
				mu.Lock()
				errs[id] = err
				mu.Unlock()
				done(id, err)
			}
			for _, d := range deps[id] {
				ch, ok := finished[d]
				if !ok {
					continue
				}
				select {
				case <-ch:
				case <-ctx.Done():
				}
				mu.Lock()
				failed := errs[d] != nil
				mu.Unlock()
				if failed {
					result(ErrAIUpstream)
					return
				}
			}
			if ctx.Err() != nil {
				result(ctx.Err())
				return
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				result(ctx.Err())
				return
			}
			err := run(ctx, id)
			<-slots
			result(err)
		}()
	}
	wg.Wait()
}
