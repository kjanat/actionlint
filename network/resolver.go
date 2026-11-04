package network

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"actionlint.kjanat.dev/v2/resolver"
)

// ResolverLookup distinguishes host cache hits and opaque delegated lookups from
// actual provider-reported attempts. Invalid auxiliary receipts are treated as
// opaque rather than changing an otherwise valid dependency resolution.
func ResolverLookup(ctx context.Context, receipt *resolver.NetworkReport, cached bool) {
	session := FromContext(ctx)
	if session == nil {
		return
	}
	op, _ := ctx.Value(operationKey{}).(Operation)
	if op.Priority == "" {
		op.Priority = Required
	}
	opaque := groupKey{"unknown", UnknownAuthentication, Delegated, "resolver", op.Priority}
	session.mu.Lock()
	defer session.mu.Unlock()
	lookup := session.groups[opaque]
	lookup.Lookups++
	session.groups[opaque] = lookup
	if cached {
		stats := session.groups[opaque]
		stats.CacheHits++
		session.groups[opaque] = stats
		return
	}
	valid := receipt != nil
	deltas := map[groupKey]Group{}
	if valid {
		for _, query := range receipt.Queries {
			if validRequest(&http.Request{URL: &url.URL{Host: query.Host}}) != nil || query.Failures > query.Count {
				valid = false
				break
			}
			kind, auth := Kind(query.Kind), Authentication(query.Authenticated)
			switch kind {
			case HTTP, REST, GraphQL, Git, OCI:
			case Delegated:
				valid = false
			default:
				valid = false
			}
			switch auth {
			case Anonymous, Credentialed, UnknownAuthentication:
			default:
				valid = false
			}
			if !valid {
				break
			}
			key := groupKey{strings.ToLower(query.Host), auth, kind, "resolver", op.Priority}
			delta := deltas[key]
			if query.Count > ^uint64(0)-delta.Attempts || query.Count+delta.Attempts > ^uint64(0)-session.groups[key].Attempts {
				valid = false
				break
			}
			delta.Attempts += query.Count
			delta.Failures += query.Failures
			deltas[key] = delta
		}
	}
	if valid {
		for key, delta := range deltas {
			stats := session.groups[key]
			stats.Attempts += delta.Attempts
			stats.Failures += delta.Failures
			stats.ProviderReported += delta.Attempts
			session.groups[key] = stats
		}
	}
	if !valid || !receipt.Complete {
		stats := session.groups[opaque]
		stats.OpaqueLookups++
		session.groups[opaque] = stats
	}
}
