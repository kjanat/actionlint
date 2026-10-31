// Package resolver defines optional network-backed dependency resolution.
package resolver

import (
	"context"
	"encoding/json"
)

// Request preserves consumer semantics: actions and workflows may resolve the
// same ref differently. ExpectedRepositoryID detects repository name reuse.
type Request struct {
	Version              int    `json:"version"`
	Repository           string `json:"repository"`
	Ref                  string `json:"ref"`
	Kind                 string `json:"kind"`
	ExpectedRepositoryID uint64 `json:"expected_repository_id,omitempty"`
}

// Result binds a resolution to immutable content and records the provider's
// evidence. Tree identities must never be presented as commit identities.
type Result struct {
	Version      int             `json:"version"`
	RepositoryID uint64          `json:"repository_id"`
	Repository   string          `json:"repository"`
	Commit       string          `json:"commit,omitempty"`
	Tree         string          `json:"tree,omitempty"`
	ObservedAt   string          `json:"observed_at"`
	Evidence     json.RawMessage `json:"evidence"`
	Error        string          `json:"error,omitempty"`
	// SelectedObject and SelectedRef retain an action profile's original object
	// and exact lookup key. Optional for existing providers; metadata acquisition
	// cannot infer a named route when this evidence is omitted.
	SelectedObject string `json:"selected_object,omitempty"`
	SelectedRef    string `json:"selected_ref,omitempty"`
}

type Resolver interface {
	Resolve(context.Context, Request) (Result, error)
}

// SessionFactory optionally scopes mutable-ref caches to one analysis request.
// Long-lived editor hosts create a new session whenever buffers invalidate analysis.
type SessionFactory interface{ NewSession() Resolver }
