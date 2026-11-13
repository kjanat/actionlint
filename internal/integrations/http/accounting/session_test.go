package accounting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, headers http.Header, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}

func request(t *testing.T, ctx context.Context, path, token string) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func quota(remaining int, reset time.Time, resource string) http.Header {
	return http.Header{"X-Ratelimit-Remaining": {strconv.Itoa(remaining)}, "X-Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)}, "X-Ratelimit-Resource": {resource}}
}

func TestTransportCountsNestedRequestsOnceAndRedacts(t *testing.T) {
	t.Parallel()
	ctx, s := TrackRequests(t.Context())
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, quota(8, time.Now().Add(time.Hour), "core"), ""), nil
	})
	transport := TrackHTTPRequests(TrackHTTPRequests(base))
	r := request(t, ctx, "/repos/private-secret?token=query-secret", "credential-secret")
	got, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	report := s.Statistics()
	if len(report.Requests) != 1 || report.Requests[0].Attempts != 1 || report.Requests[0].Credentials != CredentialsProvided || report.Requests[0].RequestKind != REST {
		t.Fatalf("%+v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), "secret") {
		t.Fatalf("unsafe report: %s %v", encoded, err)
	}
	*report.Quotas[0].Remaining = 999
	report.Requests[0].Attempts = 999
	if fresh := s.Statistics(); *fresh.Quotas[0].Remaining != 8 || fresh.Requests[0].Attempts != 1 {
		t.Fatal("snapshot aliases session storage")
	}
}

func TestReservationsProtectDependenciesAtomically(t *testing.T) {
	t.Parallel()
	ctx, s := TrackRequests(t.Context())
	reset := time.Now().Add(time.Hour)
	calls := 0
	transport := TrackHTTPRequests(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(200, quota(3, reset, "core"), ""), nil
		}
		return response(200, http.Header{}, ""), nil
	}))
	r := request(t, ctx, "/repos/example/action", "token")
	got, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	reservation, err := s.ReserveQuota(r, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Close()
	if err := s.CheckBatchQuota(request(t, reservation.Context(ctx), "/repos/required/action", "token"), 2); err != nil {
		t.Fatalf("reservation owner could not plan its batch: %v", err)
	}
	if err := s.CheckBatchQuota(r, 2); !errors.Is(err, ErrBudget) {
		t.Fatalf("unreserved batch could spend protected quota: %v", err)
	}
	optional := request(t, WithRequestOptions(ctx, RequestOptions{Purpose: "metadata", Priority: Enrichment, QuotaCost: 2}), "/repos/example/action", "token")
	got, err = transport.RoundTrip(optional)
	if got != nil {
		got.Body.Close()
	}
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("optional spent reserved quota: %v", err)
	}
	optional = request(t, WithRequestOptions(ctx, RequestOptions{Purpose: "metadata", Priority: Enrichment}), "/repos/example/action", "token")
	got, err = transport.RoundTrip(optional)
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	for range 2 {
		got, err = transport.RoundTrip(request(t, reservation.Context(ctx), "/repos/required/action", "token"))
		if err != nil {
			t.Fatal(err)
		}
		got.Body.Close()
	}
	reservation.Close()
	if calls != 4 || *s.Statistics().Quotas[0].Remaining != 0 || s.Statistics().Quotas[0].Reserved != 0 {
		t.Fatalf("calls=%d report=%+v", calls, s.Statistics())
	}
}

func TestQuotaPoolsAndSecondaryBackoff(t *testing.T) {
	t.Parallel()
	ctx, _ := TrackRequests(t.Context())
	reset := time.Now().Add(time.Hour)
	transport := TrackHTTPRequests(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/graphql":
			return response(200, quota(0, reset, "graphql"), "{}"), nil
		case "/blocked":
			h := http.Header{"Retry-After": {"120"}}
			return response(429, h, ""), nil
		case "/denied":
			return response(403, http.Header{}, ""), nil
		default:
			return response(200, quota(5, reset, "core"), ""), nil
		}
	}))
	for _, path := range []string{"/graphql", "/repos/example/action", "/denied", "/repos/example/action"} {
		got, err := transport.RoundTrip(request(t, ctx, path, "a"))
		if err != nil {
			t.Fatal(err)
		}
		got.Body.Close()
	}
	got, err := transport.RoundTrip(request(t, ctx, "/graphql", "a"))
	if got != nil {
		got.Body.Close()
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("GraphQL exhausted: %v", err)
	}
	got, err = transport.RoundTrip(request(t, ctx, "/blocked", "a"))
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	got, err = transport.RoundTrip(request(t, ctx, "/repos/example/action", "a"))
	if got != nil {
		got.Body.Close()
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("secondary limit ignored: %v", err)
	}
	got, err = transport.RoundTrip(request(t, ctx, "/repos/example/action", "b"))
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
}

func TestConcurrentAdmissionDoesNotOverspend(t *testing.T) {
	t.Parallel()
	ctx, s := TrackRequests(t.Context())
	r := request(t, ctx, "/repos/example/action", "")
	_, key, _ := requestKeys(r)
	s.budgets[key] = budget{remaining: 1, known: true, reset: time.Now().Add(time.Hour)}
	var calls atomic.Int32
	transport := TrackHTTPRequests(roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(200, http.Header{}, ""), nil
	}))
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			got, err := transport.RoundTrip(r)
			if err == nil {
				got.Body.Close()
			} else if !errors.Is(err, ErrRateLimited) {
				t.Errorf("unexpected admission error: %v", err)
			}
		})
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("sent %d requests for one available unit", calls.Load())
	}
}

func TestPreflightCoalescesAndRefreshesAfterReset(t *testing.T) {
	t.Parallel()
	ctx, s := TrackRequests(t.Context())
	now := time.Now()
	s.now = func() time.Time { return now }
	calls := 0
	client := &http.Client{Transport: TrackHTTPRequests(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/rate_limit" {
			t.Fatal("wrong probe endpoint")
		}
		return response(200, http.Header{}, fmt.Sprintf(`{"resources":{"core":{"remaining":7,"reset":%d},"graphql":{"remaining":20,"reset":%d}}}`, now.Add(time.Hour).Unix(), now.Add(time.Hour).Unix())), nil
	}))}
	r := request(t, ctx, "/repos/example/action", "a")
	for range 2 {
		if err := s.LoadGitHubRateLimits(ctx, client, r); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || len(s.Statistics().Quotas) != 2 {
		t.Fatalf("calls=%d report=%+v", calls, s.Statistics())
	}
	if err := s.CheckBatchQuota(r, 8); !errors.Is(err, ErrBudget) {
		t.Fatalf("oversized batch: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if err := s.LoadGitHubRateLimits(ctx, client, r); err != nil || calls != 2 {
		t.Fatalf("probe did not refresh: %v calls=%d", err, calls)
	}
}

func TestConcurrentPreflightSharesOneRequest(t *testing.T) {
	t.Parallel()
	ctx, s := TrackRequests(t.Context())
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	reset := time.Now().Add(time.Hour)
	client := &http.Client{Transport: TrackHTTPRequests(roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return response(http.StatusOK, quota(20, reset, "core"), "{}"), nil
	}))}
	r := request(t, ctx, "/repos/example/action", "token")
	var workers sync.WaitGroup
	run := func() {
		if err := s.LoadGitHubRateLimits(ctx, client, r); err != nil {
			t.Error(err)
		}
	}
	workers.Go(run)
	<-started
	for range 20 {
		workers.Go(run)
	}
	close(release)
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("sent %d quota requests for one credential scope", calls.Load())
	}
}

func TestPreflightWaitIsOutsideLookupBudget(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, s := TrackRequests(t.Context())
		reset := time.Now().Add(time.Hour)
		client := &http.Client{Transport: TrackHTTPRequests(roundTripFunc(func(*http.Request) (*http.Response, error) {
			time.Sleep(10 * time.Second)
			return response(http.StatusOK, quota(20, reset, "core"), "{}"), nil
		}))}
		probing := make(chan error, 1)
		go func() {
			probing <- s.LoadGitHubRateLimits(ctx, client, request(t, ctx, "/repos/example/action", "token"))
		}()
		synctest.Wait()
		budgeted, stop := references.StartLookupBudget(ctx, time.Second)
		defer stop()
		err := s.LoadGitHubRateLimits(budgeted, client, request(t, budgeted, "/repos/example/action", "token"))
		if probeErr := <-probing; err != nil || probeErr != nil || budgeted.Err() != nil {
			t.Fatalf("waiter=%v probe=%v budget=%v", err, probeErr, context.Cause(budgeted))
		}
	})
}

func TestOlderQuotaResponsesCannotRestoreSpentUnits(t *testing.T) {
	t.Parallel()
	ctx, s := TrackRequests(t.Context())
	now := time.Now().Truncate(time.Second)
	s.now = func() time.Time { return now }
	reset := now.Add(time.Hour)
	remaining := 3
	transport := TrackHTTPRequests(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, quota(remaining, reset, "core"), ""), nil
	}))
	r := request(t, ctx, "/repos/example/action", "token")
	for _, observed := range []int{3, 1, 8} {
		remaining = observed
		got, err := transport.RoundTrip(r)
		if err != nil {
			t.Fatal(err)
		}
		got.Body.Close()
	}
	if got := *s.Statistics().Quotas[0].Remaining; got != 0 {
		t.Fatalf("stale response restored quota: %d", got)
	}
	now = reset.Add(time.Second)
	reset = now.Add(time.Hour)
	remaining = 10
	got, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	got.Body.Close()
	if current := *s.Statistics().Quotas[0].Remaining; current != 10 {
		t.Fatalf("new quota window was not accepted: %d", current)
	}
}

func TestResolverCoverageIsNotInvented(t *testing.T) {
	t.Parallel()
	ctx, s := TrackRequests(t.Context())
	RecordResolverCall(ctx, nil, false)
	RecordResolverCall(ctx, nil, true)
	RecordResolverCall(ctx, &references.RequestReport{AllRequestsReported: true, Requests: []references.RequestCounts{{Host: "github.com", RequestKind: "git", Credentials: "unknown", Attempts: 2}}}, false)
	RecordResolverCall(ctx, &references.RequestReport{AllRequestsReported: true, Requests: []references.RequestCounts{{Host: "https://token@host/private?secret", RequestKind: "rest", Credentials: "provided", Attempts: 2}}}, false)
	var attempts, opaque, cached, lookups uint64
	for _, group := range s.Statistics().Requests {
		attempts += group.Attempts
		opaque += group.UnreportedResolverCalls
		cached += group.CacheHits
		lookups += group.ResolverCalls
	}
	if attempts != 2 || opaque != 2 || cached != 1 || lookups != 4 {
		t.Fatalf("attempts=%d opaque=%d cache=%d lookups=%d", attempts, opaque, cached, lookups)
	}
}
