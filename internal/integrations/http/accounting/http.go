package accounting

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"actionlint.kjanat.dev/v2/internal/integrations/network"
)

type (
	attemptKey struct{}
	attempt    struct{ active atomic.Bool }
	transport  struct{ base http.RoundTripper }
)

// TrackHTTPRequests wraps an HTTP transport. Place it below retry and authentication
// adapters to observe their individual attempts. Redirects made by http.Client
// invoke it separately. Nested wrappers count the same attempt only once.
func TrackHTTPRequests(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base}
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	return TrackHTTPAttempt(request, t.base.RoundTrip)
}

var ErrInvalidRequest = errors.New("invalid network accounting request")

func validRequest(request *http.Request) error {
	if request == nil || request.URL == nil {
		return ErrInvalidRequest
	}
	u, err := url.Parse("https://" + request.URL.Host)
	if err != nil || u.Host == "" || u.Host != request.URL.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ErrInvalidRequest
	}
	return request.Context().Err()
}

// TrackHTTPAttempt accounts for one transport attempt against its original authority,
// before a security transport rewrites the connection address to a pinned IP.
func TrackHTTPAttempt(request *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if err := validRequest(request); err != nil {
		return nil, err
	}
	if err := network.RequireOnline(request.Context()); err != nil {
		return nil, err
	}
	session := RequestTrackerFromContext(request.Context())
	if session == nil {
		return send(request)
	}
	if current, _ := request.Context().Value(attemptKey{}).(*attempt); current != nil && current.active.Load() {
		return send(request)
	}
	group, key, err := session.admit(request)
	if err != nil {
		return nil, err
	}
	current := &attempt{}
	current.active.Store(true)
	defer current.active.Store(false)
	response, err := send(request.Clone(context.WithValue(request.Context(), attemptKey{}, current)))
	session.mu.Lock()
	stats := session.groups[group]
	if err != nil || response != nil && response.StatusCode >= 400 {
		stats.Failures++
	}
	session.groups[group] = stats
	if response != nil {
		session.observe(key, response)
	}
	session.mu.Unlock()
	return response, err
}

func quotaResource(text string) bool {
	if text == "" || len(text) > 64 {
		return false
	}
	for _, c := range text {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// observe runs under the session lock. Decreasing observations in one reset
// window cannot be undone by a response that arrives out of order.
func (s *RequestTracker) observe(key quotaKey, response *http.Response) {
	now := s.now()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden && response.Header.Get("Retry-After") != "" {
		until := now.Add(time.Minute)
		if seconds, err := strconv.ParseUint(response.Header.Get("Retry-After"), 10, 32); err == nil {
			until = now.Add(time.Duration(seconds) * time.Second)
		} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil {
			until = date
		}
		if until.After(s.backoffs[key.credentialKey]) {
			s.backoffs[key.credentialKey] = until
		}
		if _, exists := s.budgets[key]; !exists {
			s.budgets[key] = budget{}
		}
	}
	if key.resource == "" {
		return
	}
	if resource := response.Header.Get("X-RateLimit-Resource"); quotaResource(resource) {
		key.resource = resource
	}
	remaining, remErr := strconv.ParseUint(response.Header.Get("X-RateLimit-Remaining"), 10, 64)
	reset, resetErr := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64)
	resetAt := time.Unix(reset, 0).UTC()
	if remErr != nil || resetErr != nil || resetAt.Year() < 1 || resetAt.Year() > 9999 {
		return
	}
	s.updateBudget(key, remaining, resetAt)
}

func (s *RequestTracker) updateBudget(key quotaKey, remaining uint64, reset time.Time) {
	b := s.budgets[key]
	if b.known && b.reset.Equal(reset) {
		remaining = min(remaining, b.remaining)
	}
	if !b.known || !reset.Before(b.reset) {
		b.remaining, b.reset, b.known = remaining, reset, true
		s.budgets[key] = b
	}
}
