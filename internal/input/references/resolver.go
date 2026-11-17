// Package references defines optional network-backed dependency resolution.
package references

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

// Request preserves consumer semantics: actions and workflows may resolve the
// same ref differently. ExpectedRepositoryID detects repository name reuse.
type Request struct {
	Version              int           `json:"version"`
	Repository           string        `json:"repository"`
	Ref                  string        `json:"ref"`
	Kind                 string        `json:"kind"`
	ExpectedRepositoryID repository.ID `json:"expected_repository_id,omitzero"`
}

// ResolutionRecord binds a resolution to immutable content and records the resolver's
// fetch log. Tree identities must never be presented as commit identities.
type ResolutionRecord struct {
	Version      int             `json:"version"`
	RepositoryID repository.ID   `json:"repository_id,omitzero"`
	Repository   string          `json:"repository"`
	Commit       string          `json:"commit,omitempty"`
	Tree         string          `json:"tree,omitempty"`
	ObservedAt   string          `json:"observed_at"`
	FetchLog     json.RawMessage `json:"fetch_log"`
	Error        string          `json:"error,omitempty"`
	// SelectedObject and SelectedRef record an action profile's original object
	// and exact lookup key. Optional for existing providers; metadata acquisition
	// cannot infer a named route when these fields are omitted.
	SelectedObject string                    `json:"selected_object,omitempty"`
	SelectedRef    string                    `json:"selected_ref,omitempty"`
	Network        *accounting.RequestReport `json:"network,omitempty"`
}

type Resolver interface {
	Resolve(context.Context, Request) (ResolutionRecord, error)
}

// SessionFactory optionally scopes mutable-ref caches to one analysis request.
// Long-lived editor hosts create a new session whenever buffers invalidate analysis.
type SessionFactory interface{ NewSession() Resolver }
