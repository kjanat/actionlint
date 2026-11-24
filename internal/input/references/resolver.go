// Package references defines optional network-backed dependency resolution.
package references

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp/accounting"
	"actionlint.kjanat.dev/v2/internal/model/repository"
)

// Request uses ExpectedRepositoryID to detect repository name reuse.
type Request struct {
	Version              int           `json:"version"`
	Repository           string        `json:"repository"`
	Ref                  string        `json:"ref"`
	Kind                 string        `json:"kind"`
	ExpectedRepositoryID repository.ID `json:"expected_repository_id,omitzero"`
}

// ResolutionRecord must never present tree identities as commit identities.
type ResolutionRecord struct {
	Version      int             `json:"version"`
	RepositoryID repository.ID   `json:"repository_id,omitzero"`
	Repository   string          `json:"repository"`
	Commit       string          `json:"commit,omitempty"`
	Tree         string          `json:"tree,omitempty"`
	ObservedAt   string          `json:"observed_at"`
	FetchLog     json.RawMessage `json:"fetch_log"`
	Error        string          `json:"error,omitempty"`
	// Metadata acquisition cannot infer a named route without SelectedObject and SelectedRef.
	SelectedObject string                    `json:"selected_object,omitempty"`
	SelectedRef    string                    `json:"selected_ref,omitempty"`
	Network        *accounting.RequestReport `json:"network,omitempty"`
}

type Resolver interface {
	Resolve(context.Context, Request) (ResolutionRecord, error)
}

type SessionFactory interface{ NewSession() Resolver }
