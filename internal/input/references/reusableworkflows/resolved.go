package reusableworkflows

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/model/calls"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
)

type ResolvedWorkflows struct{ cache *MetadataCache }

func (c *MetadataCache) Resolved() *ResolvedWorkflows { return &ResolvedWorkflows{cache: c} }

// LookupForCall returns nil when no definition was acquired for this call.
func (r *ResolvedWorkflows) LookupForCall(spec string) (*calls.Metadata, error) {
	if r == nil || r.cache == nil {
		return nil, nil
	}
	if IsRemoteReference(spec) {
		return r.cache.remote.Lookup(spec)
	}
	key, local := callreferences.LocalWorkflow(spec)
	if !local {
		return nil, nil
	}
	metadata, err, _ := r.cache.Lookup(key)
	if failure, ok := errors.AsType[metadataError](err); ok {
		return nil, failure.at(spec)
	}
	return metadata, err
}
