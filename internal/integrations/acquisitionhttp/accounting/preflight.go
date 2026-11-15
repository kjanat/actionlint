package accounting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
)

// LoadGitHubRateLimits obtains quota information once per credential scope before a
// batch. It does not discover credentials or grant permission to use the network.
// The caller must already have selected online access and supplied its client.
// An unavailable quota endpoint leaves the quota unknown; it does not invent a
// zero allowance. Response headers on subsequent requests refine the estimate.
func (s *RequestTracker) LoadGitHubRateLimits(ctx context.Context, client *http.Client, request *http.Request) error {
	if err := validRequest(request); err != nil {
		return err
	}
	if !strings.EqualFold(request.URL.Host, "api.github.com") || request.URL.Scheme != "https" {
		return nil
	}
	_, key, _ := requestKeys(request)
	s.mu.Lock()
	prior := s.rateLimitLoads[key.credentialKey]
	if prior != nil {
		select {
		case <-prior.done:
			b := s.budgets[key]
			if !b.known || b.reset.After(s.now()) {
				s.mu.Unlock()
				return ctx.Err()
			}
		default:
			s.mu.Unlock()
			return references.WaitOutsideLookupBudget(ctx, func() error {
				select {
				case <-prior.done:
					return ctx.Err()
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}
	}
	current := &rateLimitLoad{done: make(chan struct{})}
	s.rateLimitLoads[key.credentialKey] = current
	s.mu.Unlock()
	defer close(current.done)
	probeContext := WithRequestOptions(ctx, RequestOptions{Purpose: "quota-preflight", Priority: Control, RequestKind: REST, QuotaPool: "core"})
	probeRequest, err := http.NewRequestWithContext(probeContext, http.MethodGet, "https://api.github.com/rate_limit", nil)
	if err != nil {
		return err
	}
	probeRequest.Header.Set("Accept", "application/vnd.github+json")
	probeRequest.Header.Set("User-Agent", "actionlint")
	probeRequest.Header.Set("Authorization", request.Header.Get("Authorization"))
	response, err := client.Do(probeRequest)
	if err != nil {
		return ctx.Err()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ctx.Err()
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return ctx.Err()
	}
	var result struct {
		Resources map[string]struct {
			Remaining *uint64 `json:"remaining"`
			Reset     *int64  `json:"reset"`
		} `json:"resources"`
	}
	if json.Unmarshal(body, &result) != nil || len(result.Resources) > 64 {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for resource, quota := range result.Resources {
		if !quotaResource(resource) || quota.Remaining == nil || quota.Reset == nil {
			continue
		}
		reset := time.Unix(*quota.Reset, 0).UTC()
		if reset.Year() < 1 || reset.Year() > 9999 {
			continue
		}
		pool := key
		pool.resource = resource
		s.updateBudget(pool, *quota.Remaining, reset)
	}
	return ctx.Err()
}

// CheckBatchQuota tests a batch's planned cost without spending quota. Individual
// requests still pass admission, because another operation can spend it first.
func (s *RequestTracker) CheckBatchQuota(request *http.Request, cost uint64) error {
	if err := validRequest(request); err != nil {
		return err
	}
	group, key, _ := requestKeys(request)
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.budgets[key]
	protected := b.reserved
	if reservation, _ := request.Context().Value(quotaReservationKey{}).(*QuotaReservation); reservation != nil && reservation.session == s && reservation.key == key && !reservation.closed {
		protected -= reservation.remaining
	}
	err := s.checkBudget(group, key, b, cost, protected, s.now())
	if err != nil {
		stats := s.groups[group]
		stats.QuotaRejections++
		s.groups[group] = stats
	}
	return err
}
