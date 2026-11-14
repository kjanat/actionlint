// Package network controls network access for one actionlint invocation.
package network

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
)

var ErrOffline = errors.New("network access is disabled by --offline")

type Options struct {
	Offline        bool
	CacheDirectory string
	WarmCache      bool
}

type Session struct {
	Options
	mu            sync.Mutex
	warnings      []string
	cacheFailures []error
}

type sessionKey struct{}

func WithSession(ctx context.Context, options Options) (context.Context, *Session) {
	s := &Session{Options: options}
	return context.WithValue(ctx, sessionKey{}, s), s
}

func Settings(ctx context.Context) Options {
	if s, ok := ctx.Value(sessionKey{}).(*Session); ok {
		return s.Options
	}
	return Options{}
}

func RequireOnline(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if Settings(ctx).Offline {
		return ErrOffline
	}
	return nil
}

// OnlyOffline rejects joined errors that also contain a separate failure.
func OnlyOffline(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !OnlyOffline(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return OnlyOffline(wrapped.Unwrap())
	}
	return errors.Is(err, ErrOffline)
}

func Missing(resource string) error {
	if u, err := url.Parse(resource); err == nil && u.Host != "" {
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		resource = u.String()
	}
	return fmt.Errorf("%s is not cached: %w", resource, ErrOffline)
}

func Warn(ctx context.Context, message string) {
	if s, ok := ctx.Value(sessionKey{}).(*Session); ok {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !slices.Contains(s.warnings, message) {
			s.warnings = append(s.warnings, message)
		}
	}
}

func (s *Session) Warnings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.warnings)
}

func CacheFailed(ctx context.Context, resource string) {
	if s, ok := ctx.Value(sessionKey{}).(*Session); ok && s.WarmCache {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.cacheFailures = append(s.cacheFailures, fmt.Errorf("could not cache %s", resource))
	}
}

func CacheError(ctx context.Context) error {
	if s, ok := ctx.Value(sessionKey{}).(*Session); ok {
		s.mu.Lock()
		defer s.mu.Unlock()
		return errors.Join(s.cacheFailures...)
	}
	return nil
}
