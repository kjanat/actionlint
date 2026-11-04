package network

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type (
	sessionKey     struct{}
	operationKey   struct{}
	reservationKey struct{}
)

type (
	groupKey struct {
		host     string
		auth     Authentication
		kind     Kind
		purpose  string
		priority Priority
	}
	credentialKey struct {
		host       string
		auth       Authentication
		credential [32]byte
	}
	quotaKey struct {
		credentialKey
		resource string
	}
	budget struct {
		remaining, reserved uint64
		reset               time.Time
		known               bool
	}
	probe struct{ done chan struct{} }
)

// Session owns counters and quota estimates for one invocation. Each Snapshot is
// detached. Other processes can spend the same server quota between observations.
type Session struct {
	mu       sync.Mutex
	groups   map[groupKey]Group
	budgets  map[quotaKey]budget
	probes   map[credentialKey]*probe
	backoffs map[credentialKey]time.Time
	now      func() time.Time
}

// New creates an invocation session with no assumed quota information.
func New() *Session {
	return &Session{groups: map[groupKey]Group{}, budgets: map[quotaKey]budget{}, probes: map[credentialKey]*probe{}, backoffs: map[credentialKey]time.Time{}, now: time.Now}
}

// WithSession shares accounting and reservations across acquisition clients.
func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

// FromContext returns the supplied session, or nil when the caller supplied none.
func FromContext(ctx context.Context) *Session {
	session, _ := ctx.Value(sessionKey{}).(*Session)
	return session
}

// Ensure shares an existing invocation session or creates a new one.
func Ensure(ctx context.Context) (context.Context, *Session) {
	if session := FromContext(ctx); session != nil {
		return ctx, session
	}
	session := New()
	return WithSession(ctx, session), session
}

// WithOperation labels subsequent requests without changing network authority.
func WithOperation(ctx context.Context, operation Operation) context.Context {
	return context.WithValue(ctx, operationKey{}, operation)
}

func requestKeys(request *http.Request) (groupKey, quotaKey, uint64) {
	operation, _ := request.Context().Value(operationKey{}).(Operation)
	if operation.Priority == "" {
		operation.Priority = Required
	}
	if operation.Purpose == "" {
		operation.Purpose = "acquisition"
	}
	host := strings.ToLower(request.URL.Host)
	kind := operation.Kind
	if kind == "" {
		kind = HTTP
		if strings.EqualFold(request.URL.Hostname(), "api.github.com") {
			kind = REST
			if request.URL.Path == "/graphql" {
				kind = GraphQL
			}
		}
	}
	auth := Anonymous
	credential := request.Header.Get("Authorization")
	if credential != "" || request.URL.User != nil {
		auth = Credentialed
	}
	if request.URL.User != nil {
		credential += "\x00" + request.URL.User.String()
	}
	resource := ""
	if kind == REST {
		resource = "core"
		if strings.HasPrefix(request.URL.Path, "/search/") {
			resource = "search"
		}
	}
	if kind == GraphQL {
		resource = "graphql"
	}
	if operation.Resource != "" && quotaResource(operation.Resource) {
		resource = operation.Resource
	}
	cost := operation.Cost
	if cost == 0 && kind == REST {
		cost = 1
	}
	if operation.Priority == Control {
		cost = 0
	}
	return groupKey{host, auth, kind, operation.Purpose, operation.Priority}, quotaKey{credentialKey{host, auth, sha256.Sum256([]byte(credential))}, resource}, cost
}

var (
	ErrBudget      = errors.New("network request exceeds available or unreserved quota")
	ErrRateLimited = errors.New("network request blocked by an observed rate limit")
)

func (s *Session) admit(request *http.Request) (groupKey, quotaKey, error) {
	group, key, cost := requestKeys(request)
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.budgets[key]
	now := s.now()
	reservation, _ := request.Context().Value(reservationKey{}).(*Reservation)
	owned := uint64(0)
	protected := b.reserved
	if reservation != nil && reservation.session == s && reservation.key == key && !reservation.closed {
		owned = min(cost, reservation.remaining)
		protected -= reservation.remaining
	}
	err := s.checkBudget(group, key, b, cost, protected, now)
	stats := s.groups[group]
	if err != nil {
		stats.Blocked++
	} else {
		stats.Attempts++
		if owned != 0 {
			reservation.remaining -= owned
			b.reserved -= owned
			s.budgets[key] = b
		}
		if b.known && b.reset.After(now) && cost <= b.remaining {
			b.remaining -= cost
			s.budgets[key] = b
		}
	}
	s.groups[group] = stats
	return group, key, err
}

// checkBudget runs under the session lock for both batch checks and requests.
func (s *Session) checkBudget(group groupKey, key quotaKey, b budget, cost, protected uint64, now time.Time) error {
	switch {
	case s.backoffs[key.credentialKey].After(now):
		return ErrRateLimited
	case group.priority == Control:
		return nil
	case b.known && b.remaining == 0 && b.reset.After(now):
		return ErrRateLimited
	case b.reserved != 0 && group.priority == Enrichment && (!b.known || !b.reset.After(now)):
		return ErrBudget
	case b.known && b.reset.After(now) && (cost > b.remaining || b.remaining-cost < protected || cost == 0 && b.reserved != 0):
		return ErrBudget
	default:
		return nil
	}
}

// Reservation protects future work in one credential and quota resource scope.
// Use Context for its requests and Close when the work finishes or is cancelled.
type Reservation struct {
	session   *Session
	key       quotaKey
	remaining uint64
	closed    bool
}

// Context authorizes requests to consume this reservation atomically at admission.
func (r *Reservation) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, reservationKey{}, r)
}

// Close releases unused units. Repeated calls have no effect.
func (r *Reservation) Close() {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	if !r.closed {
		b := r.session.budgets[r.key]
		b.reserved -= r.remaining
		r.session.budgets[r.key] = b
		r.remaining = 0
		r.closed = true
	}
}

// Reserve protects known future dependency work from unreserved requests in the
// same quota pool. Reservation units are not consumed network requests.
func (s *Session) Reserve(request *http.Request, units uint64) (*Reservation, error) {
	if err := validRequest(request); err != nil {
		return nil, err
	}
	_, key, _ := requestKeys(request)
	s.mu.Lock()
	b := s.budgets[key]
	if units > ^uint64(0)-b.reserved || b.known && b.reset.After(s.now()) && (b.reserved > b.remaining || units > b.remaining-b.reserved) {
		s.mu.Unlock()
		return nil, ErrBudget
	}
	b.reserved += units
	s.budgets[key] = b
	s.mu.Unlock()
	return &Reservation{session: s, key: key, remaining: units}, nil
}

// Snapshot returns detached counters and quota observations in a stable order.
func (s *Session) Snapshot() Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	report := Report{Groups: []Group{}, Quotas: []Quota{}}
	for key, value := range s.groups {
		value.Host, value.Authentication, value.Kind, value.Purpose, value.Priority = key.host, key.auth, key.kind, key.purpose, key.priority
		report.Groups = append(report.Groups, value)
	}
	for key, value := range s.budgets {
		until := s.backoffs[key.credentialKey]
		quota := Quota{Host: key.host, Authentication: key.auth, Resource: key.resource, Reserved: value.reserved}
		if !until.IsZero() {
			quota.RetryAfter = new(until)
		}
		if value.known {
			quota.Remaining, quota.Reset = new(value.remaining), new(value.reset)
		}
		if value.known || value.reserved != 0 || until.After(s.now()) {
			report.Quotas = append(report.Quotas, quota)
		}
	}
	slices.SortFunc(report.Groups, func(a, b Group) int {
		return strings.Compare(a.Host+"\x00"+string(a.Authentication)+"\x00"+string(a.Kind)+"\x00"+a.Purpose+"\x00"+string(a.Priority), b.Host+"\x00"+string(b.Authentication)+"\x00"+string(b.Kind)+"\x00"+b.Purpose+"\x00"+string(b.Priority))
	})
	slices.SortFunc(report.Quotas, func(a, b Quota) int {
		if order := strings.Compare(a.Host+"\x00"+string(a.Authentication)+"\x00"+a.Resource, b.Host+"\x00"+string(b.Authentication)+"\x00"+b.Resource); order != 0 {
			return order
		}
		if a.Remaining == nil {
			if b.Remaining == nil {
				if a.RetryAfter == nil {
					if b.RetryAfter == nil {
						return 0
					}
					return -1
				}
				if b.RetryAfter == nil {
					return 1
				}
				return a.RetryAfter.Compare(*b.RetryAfter)
			}
			return -1
		}
		if b.Remaining == nil {
			return 1
		}
		if *a.Remaining < *b.Remaining {
			return -1
		}
		if *a.Remaining > *b.Remaining {
			return 1
		}
		return a.Reset.Compare(*b.Reset)
	})
	return report
}
