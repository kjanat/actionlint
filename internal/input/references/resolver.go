// Package references defines optional network-backed dependency resolution.
package references

import (
	"context"
	"encoding/json"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

// Request preserves consumer semantics: actions and workflows may resolve the
// same ref differently. ExpectedRepositoryID detects repository name reuse.
type Request struct {
	Version              int             `json:"version"`
	Repository           string          `json:"repository"`
	Ref                  string          `json:"ref"`
	Kind                 string          `json:"kind"`
	ExpectedRepositoryID repositoryid.ID `json:"expected_repository_id,omitzero"`
}

// Result binds a resolution to immutable content and records the resolver's
// fetch log. Tree identities must never be presented as commit identities.
type Result struct {
	Version      int             `json:"version"`
	RepositoryID repositoryid.ID `json:"repository_id,omitzero"`
	Repository   string          `json:"repository"`
	Commit       string          `json:"commit,omitempty"`
	Tree         string          `json:"tree,omitempty"`
	ObservedAt   string          `json:"observed_at"`
	FetchLog     json.RawMessage `json:"fetch_log"`
	Error        string          `json:"error,omitempty"`
	// SelectedObject and SelectedRef record an action profile's original object
	// and exact lookup key. Optional for existing providers; metadata acquisition
	// cannot infer a named route when these fields are omitted.
	SelectedObject string         `json:"selected_object,omitempty"`
	SelectedRef    string         `json:"selected_ref,omitempty"`
	Network        *RequestReport `json:"network,omitempty"`
}

// RequestCounts reports actual attempts made by an external resolver. Host is an
// authority without credentials, paths or queries. RequestKind is http, rest,
// graphql, git or oci. Credentials is provided, absent or unknown and describes
// whether credentials were supplied, regardless of whether the server accepted them.
type RequestCounts struct {
	Host        string `json:"host"`
	RequestKind string `json:"request_kind"`
	Credentials string `json:"credentials"`
	Attempts    uint64 `json:"attempts"`
	Failures    uint64 `json:"failures"`
}

// RequestReport reports a resolver's network activity. AllRequestsReported means every remote
// attempt is included, including failed attempts. Omission does not mean zero.
type RequestReport struct {
	AllRequestsReported bool            `json:"all_requests_reported"`
	Requests            []RequestCounts `json:"requests"`
}

// Clone detaches every mutable field, including optional network receipts.
func (r Result) Clone() Result {
	r.FetchLog = slices.Clone(r.FetchLog)
	if r.Network != nil {
		r.Network = &RequestReport{AllRequestsReported: r.Network.AllRequestsReported, Requests: slices.Clone(r.Network.Requests)}
	}
	return r
}

type Resolver interface {
	Resolve(context.Context, Request) (Result, error)
}

// SessionFactory optionally scopes mutable-ref caches to one analysis request.
// Long-lived editor hosts create a new session whenever buffers invalidate analysis.
type SessionFactory interface{ NewSession() Resolver }
