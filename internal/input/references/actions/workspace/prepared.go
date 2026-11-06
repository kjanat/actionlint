package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

type preparedAction struct {
	spec            string
	result          MetadataResult
	document        SourceDocument
	cached          bool
	diagnosticError error
}

// PreparedActions reads the action acquired for the current execution step.
// It cannot resolve paths, read manifests or change checkout state.
type PreparedActions struct{ resolver *Resolver }

func (c *Resolver) Prepared() *PreparedActions { return &PreparedActions{resolver: c} }

// PrepareMetadata acquires a reference after the caller applies the step's
// checkout state, before rule callbacks observe that step.
func (c *Resolver) PrepareMetadata(ctx context.Context, spec string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result, cached := c.LookupMetadata(spec)
	if errors.Is(result.Err(), context.Canceled) || errors.Is(result.Err(), context.DeadlineExceeded) || errors.Is(result.Err(), os.ErrPermission) {
		return result.Err()
	}
	document, _ := c.SourceDocument(result)
	if err := ctx.Err(); err != nil {
		return err
	}
	if c != nil {
		c.mu.Lock()
		c.prepared = preparedAction{spec: spec, result: result, document: document, cached: cached, diagnosticError: c.base.reportMetadataError(result)}
		c.mu.Unlock()
	}
	return nil
}

// LookupMetadata returns the prepared acquisition result without filesystem IO.
func (r *PreparedActions) LookupMetadata(spec string) (MetadataResult, bool) {
	prepared := r.lookup(spec)
	return prepared.result, prepared.cached
}

func (r *PreparedActions) lookup(spec string) preparedAction {
	if r == nil || r.resolver == nil {
		return preparedAction{result: MetadataResult{status: MetadataLookupDisabled}}
	}
	c := r.resolver
	c.mu.RLock()
	prepared := c.prepared
	c.mu.RUnlock()
	if prepared.spec != spec || spec == "" {
		err := fmt.Errorf("action %q was not prepared for this step", spec)
		return preparedAction{result: MetadataResult{status: MetadataReadFailed, cause: err}, diagnosticError: err}
	}
	if prepared.document.captured.Valid() {
		if err := c.base.captures.VerifyHandle(prepared.document.captured); err != nil {
			return preparedAction{result: MetadataResult{status: MetadataReadFailed, cause: err}, cached: true, diagnosticError: err}
		}
	}
	return prepared
}

func (r *PreparedActions) FindMetadata(spec string) (*actionmeta.Metadata, bool, error) {
	metadata, cached, _, err := r.FindMetadataDocument(spec)
	return metadata, cached, err
}

func (r *PreparedActions) FindMetadataDocument(spec string) (*actionmeta.Metadata, bool, SourceDocument, error) {
	prepared := r.lookup(spec)
	return prepared.result.Metadata(), prepared.cached, prepared.document, prepared.diagnosticError
}
