// Package resolver defines optional network-backed dependency resolution.
package resolver

import (
	"context"
	"encoding/json"
	"slices"
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
	SelectedObject string         `json:"selected_object,omitempty"`
	SelectedRef    string         `json:"selected_ref,omitempty"`
	Network        *NetworkReport `json:"network,omitempty"`
}

// NetworkQuery reports actual attempts made by an external resolver. Host is an
// authority without credentials, paths or queries. Kind is http, rest, graphql,
// git or oci. Authenticated is yes, no or unknown and describes supplied credentials.
type NetworkQuery struct {
	Host          string `json:"host"`
	Kind          string `json:"kind"`
	Authenticated string `json:"authenticated"`
	Count         uint64 `json:"count"`
	Failures      uint64 `json:"failures"`
}

// NetworkReport is additive to protocol version 1. Complete means every remote
// attempt is included, including failed attempts. Omission does not mean zero.
type NetworkReport struct {
	Complete bool           `json:"complete"`
	Queries  []NetworkQuery `json:"queries"`
}

// Clone detaches every mutable field, including optional network receipts.
func (r Result) Clone() Result {
	r.Evidence = slices.Clone(r.Evidence)
	if r.Network != nil {
		r.Network = &NetworkReport{Complete: r.Network.Complete, Queries: slices.Clone(r.Network.Queries)}
	}
	return r
}

type Resolver interface {
	Resolve(context.Context, Request) (Result, error)
}

// SessionFactory optionally scopes mutable-ref caches to one analysis request.
// Long-lived editor hosts create a new session whenever buffers invalidate analysis.
type SessionFactory interface{ NewSession() Resolver }
