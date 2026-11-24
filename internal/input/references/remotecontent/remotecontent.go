// Package remotecontent shares remote metadata acquisition within one analysis request.
package remotecontent

import (
	"context"
	"crypto/sha256"
	"errors"
	"maps"
	"strings"
	"sync"
)

// Location repository names compare without case.
type Location struct {
	Host, Repository, Commit, Path string
}

type Content struct {
	Location Location
	Name     string
	Bytes    string
}

// Store keeps successful results for the request, shares failures with
// concurrent waiters, and retries failed keys on the next acquisition.
type Store[K comparable, R, P any] struct {
	parse    func([]byte) P
	mu       sync.Mutex
	results  map[K]R
	pending  map[K]*flight[R]
	contents map[Location]parsed[P]
}

type flight[R any] struct {
	ready   chan struct{}
	result  R
	failure failure
}

type parsed[P any] struct {
	digest [sha256.Size]byte
	value  P
}

func New[K comparable, R, P any](parse func([]byte) P) *Store[K, R, P] {
	return &Store[K, R, P]{parse: parse, results: make(map[K]R), pending: make(map[K]*flight[R]), contents: make(map[Location]parsed[P])}
}

// Acquire runs again for a live waiter when the running acquisition was canceled.
func (s *Store[K, R, P]) Acquire(ctx context.Context, key K, acquire func(context.Context) (R, error)) (R, error) {
	var zero R
	for {
		s.mu.Lock()
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return zero, err
		}
		if result, ok := s.results[key]; ok {
			s.mu.Unlock()
			return result, nil
		}
		if running, exists := s.pending[key]; exists {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-running.ready:
			}
			if err := ctx.Err(); err != nil {
				return zero, err
			}
			if running.failure.canceled || running.failure.deadline {
				// The acquiring caller's context ended; this caller's has not.
				continue
			}
			return running.result, running.failure.err()
		}
		running := &flight[R]{ready: make(chan struct{})}
		s.pending[key] = running
		s.mu.Unlock()
		result, err := acquire(ctx)
		s.mu.Lock()
		if err == nil {
			s.results[key] = result
		}
		running.result, running.failure = result, failureOf(err)
		delete(s.pending, key)
		close(running.ready)
		s.mu.Unlock()
		return result, running.failure.err()
	}
}

func (s *Store[K, R, P]) Lookup(key K) (R, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[key]
	return result, ok
}

func (s *Store[K, R, P]) Results() map[K]R {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.results)
}

// Parse reports false when the location was already seen with other content.
func (s *Store[K, R, P]) Parse(content Content) (P, bool) {
	location := content.Location
	location.Repository = strings.ToLower(location.Repository)
	digest := sha256.Sum256([]byte(content.Name + "\x00" + content.Bytes))
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, exists := s.contents[location]; exists {
		var zero P
		if previous.digest != digest {
			return zero, false
		}
		return previous.value, true
	}
	value := s.parse([]byte(content.Bytes))
	s.contents[location] = parsed[P]{digest: digest, value: value}
	return value, true
}

// failure is an immutable copy of an acquisition error.
type failure struct {
	message                     string
	present, canceled, deadline bool
}

func failureOf(err error) failure {
	if err == nil {
		return failure{}
	}
	return failure{message: err.Error(), present: true, canceled: errors.Is(err, context.Canceled), deadline: errors.Is(err, context.DeadlineExceeded)}
}

func (f failure) Error() string { return f.message }

func (f failure) Is(target error) bool {
	return f.canceled && target == context.Canceled || f.deadline && target == context.DeadlineExceeded
}

func (f failure) err() error {
	if !f.present {
		return nil
	}
	return f
}
