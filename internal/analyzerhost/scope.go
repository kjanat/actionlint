// Package analyzerhost owns analyzer execution and resource lifetimes.
package analyzerhost

import (
	"errors"
	"sync"
)

// Scope joins resources created for one analysis, including on early failures.
// Register before Finish. Every join runs even if an earlier join fails.
// Concurrent Finish calls wait for the same completion and return the same error.
type Scope struct {
	mu    sync.Mutex
	joins []func() error
	done  chan struct{}
	err   error
}

func (s *Scope) Register(join func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		panic("registering an analyzer resource after scope completion")
	}
	s.joins = append(s.joins, join)
}

func (s *Scope) Finish() error {
	s.mu.Lock()
	if s.done != nil {
		done := s.done
		s.mu.Unlock()
		<-done
		return s.err
	}
	s.done = make(chan struct{})
	joins := s.joins
	s.mu.Unlock()
	var failures []error
	for _, join := range joins {
		failures = append(failures, join())
	}
	s.err = errors.Join(failures...)
	close(s.done)
	return s.err
}
