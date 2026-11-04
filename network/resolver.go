package network

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"actionlint.kjanat.dev/v2/resolver"
)

// RecordResolverCall distinguishes host cache hits and opaque delegated lookups from
// actual provider-reported attempts. Invalid auxiliary receipts are treated as
// opaque rather than changing an otherwise valid dependency resolution.
func RecordResolverCall(ctx context.Context, receipt *resolver.RequestReport, cached bool) {
	session := RequestTrackerFromContext(ctx)
	if session == nil {
		return
	}
	op, _ := ctx.Value(requestOptionsKey{}).(RequestOptions)
	if op.Priority == "" {
		op.Priority = Required
	}
	opaque := groupKey{"unknown", CredentialsUnknown, Delegated, "resolver", op.Priority}
	session.mu.Lock()
	defer session.mu.Unlock()
	lookup := session.groups[opaque]
	lookup.ResolverCalls++
	session.groups[opaque] = lookup
	if cached {
		stats := session.groups[opaque]
		stats.CacheHits++
		session.groups[opaque] = stats
		return
	}
	valid := receipt != nil
	deltas := map[groupKey]RequestCounts{}
	if valid {
		for _, query := range receipt.Requests {
			if validRequest(&http.Request{URL: &url.URL{Host: query.Host}}) != nil || query.Failures > query.Attempts {
				valid = false
				break
			}
			kind, auth := RequestKind(query.RequestKind), CredentialStatus(query.Credentials)
			switch kind {
			case HTTP, REST, GraphQL, Git, OCI:
			case Delegated:
				valid = false
			default:
				valid = false
			}
			switch auth {
			case NoCredentials, CredentialsProvided, CredentialsUnknown:
			default:
				valid = false
			}
			if !valid {
				break
			}
			key := groupKey{strings.ToLower(query.Host), auth, kind, "resolver", op.Priority}
			delta := deltas[key]
			if query.Attempts > ^uint64(0)-delta.Attempts || query.Attempts+delta.Attempts > ^uint64(0)-session.groups[key].Attempts {
				valid = false
				break
			}
			delta.Attempts += query.Attempts
			delta.Failures += query.Failures
			deltas[key] = delta
		}
	}
	if valid {
		for key, delta := range deltas {
			stats := session.groups[key]
			stats.Attempts += delta.Attempts
			stats.Failures += delta.Failures
			stats.ResolverReportedAttempts += delta.Attempts
			session.groups[key] = stats
		}
	}
	if !valid || !receipt.AllRequestsReported {
		stats := session.groups[opaque]
		stats.UnreportedResolverCalls++
		session.groups[opaque] = stats
	}
}
