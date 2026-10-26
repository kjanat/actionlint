package local

import (
	"errors"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
)

type MetadataStatus uint8

const (
	MetadataInvalid MetadataStatus = iota
	MetadataResolved
	MetadataDockerfileOnly
	MetadataLookupDisabled
	MetadataNonLocal
	MetadataNotFetched
	MetadataPlacementUncertain
	MetadataMissingManifest
	MetadataReadFailed
	MetadataParseFailed
	MetadataMappingFailed
)

// MetadataResult retains acquisition facts independently of diagnostic emission.
type MetadataResult struct {
	status    MetadataStatus
	metadata  *actionmeta.Metadata
	cause     error
	cacheSpec string
}

func (r MetadataResult) Status() MetadataStatus         { return r.status }
func (r MetadataResult) Metadata() *actionmeta.Metadata { return r.metadata }
func (r MetadataResult) Err() error                     { return r.cause }

type metadataEntry struct {
	result   MetadataResult
	reported bool
}

func (c *Cache) reportMetadataError(result MetadataResult) error {
	if result.cause == nil || result.cacheSpec == "" {
		return result.cause
	}
	if _, missing := errors.AsType[UnresolvedError](result.cause); missing {
		return result.cause
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.cache[result.cacheSpec]
	if entry.reported {
		return nil
	}
	entry.reported = true
	c.cache[result.cacheSpec] = entry
	return result.cause
}
