package ruff

import (
	"context"
	"errors"
	"sync"
)

// versionProbe shares completed discovery results while keeping cancellation
// local to each caller. An interrupted probe leaves discovery retryable.
type versionProbe struct {
	mu      sync.Mutex
	pending chan struct{}
	ready   bool
	output  string
	err     error
}

func (p *versionProbe) version() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.output
}

func (p *versionProbe) run(ctx context.Context, execute func() (string, error), warning func(error)) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		p.mu.Lock()
		if p.ready {
			output, err := p.output, p.err
			p.mu.Unlock()
			return output, err
		}
		if pending := p.pending; pending != nil {
			p.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		p.pending = pending
		p.mu.Unlock()
		output, err := execute()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		completed := !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
		p.mu.Lock()
		if completed {
			p.ready, p.output, p.err = true, output, err
		}
		p.pending = nil
		close(pending)
		p.mu.Unlock()
		if completed && err != nil && warning != nil {
			warning(err)
		}
		return output, err
	}
}
