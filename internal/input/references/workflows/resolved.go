package workflows

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/references"
)

// ResolvedWorkflows reads acquired metadata without loading manifests or changing the cache.
type ResolvedWorkflows struct{ cache *Cache }

// Resolved supplies a lookup-only view. Preparation can add metadata before checks run.
func (c *Cache) Resolved() *ResolvedWorkflows { return &ResolvedWorkflows{cache: c} }

// LookupForCall returns acquired metadata or an input failure using the caller's
// reference spelling. A nil result means no definition was acquired for this call.
func (r *ResolvedWorkflows) LookupForCall(spec string) (*calls.Metadata, error) {
	if r == nil || r.cache == nil {
		return nil, nil
	}
	key, local := references.LocalWorkflow(spec)
	if !local {
		return nil, nil
	}
	metadata, err, _ := r.cache.Lookup(key)
	if failure, ok := errors.AsType[metadataError](err); ok {
		return nil, failure.at(spec)
	}
	return metadata, err
}
