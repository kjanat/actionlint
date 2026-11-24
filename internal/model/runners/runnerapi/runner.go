// Package runnerapi defines snapshot-based runner knowledge. Providers never emit diagnostics.
package runnerapi

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
)

type (
	Term       interface{ runnerTerm() }
	Literal    string
	Expression string
)

func (Literal) runnerTerm()    {}
func (Expression) runnerTerm() {}

func (v Literal) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Literal string `json:"literal"`
	}{string(v)})
}

func (v Expression) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Expression string `json:"expression"`
	}{string(v)})
}

type Constraint struct {
	Value    Term
	Position sourcetext.Position
}
type Rejection struct {
	Reason   string
	Position sourcetext.Position
}

// Selector is one conjunction. Separate selectors are alternatives, including
// matrix combinations. A nil Group means no group constraint.
type Selector struct {
	Incomplete bool
	Labels     []Constraint
	Group      *Constraint
	// Expression is the source of a selector whose labels and group are unknown.
	Expression string
}

type Freshness string

const (
	Fresh   Freshness = "fresh"
	Stale   Freshness = "stale"
	Missing Freshness = "missing"
)

// RunnerSnapshot contains runner data supplied explicitly by the host. Content is
// provider-specific data and grants no capability to access files or the network.
type RunnerSnapshot struct {
	Content    string    `json:"content"`
	Scope      string    `json:"scope"`
	ObtainedAt string    `json:"obtained_at"`
	Freshness  Freshness `json:"freshness"`
	Complete   bool      `json:"complete"`
}

func (s RunnerSnapshot) Digest() string {
	data, _ := json.Marshal(s)
	return sha256hex.Digest(data)
}

type Request struct {
	Service   services.Service
	AsOf      lifecycle.Date
	Target    string
	Scope     string
	Snapshot  RunnerSnapshot
	Selectors []Selector
}

type OS string

type CaseSensitivity string

const (
	UnknownCase     CaseSensitivity = "unknown"
	CaseSensitive   CaseSensitivity = "sensitive"
	CaseInsensitive CaseSensitivity = "insensitive"
)

const (
	UnknownOS OS = "unknown"
	Linux     OS = "linux"
	MacOS     OS = "macos"
	Windows   OS = "windows"
)

// Candidate describes explicit guarantees and never guesses from a label's spelling.
type Candidate struct {
	Hosting         Hosting
	Name            string
	OS              OS
	Architecture    string
	DefaultShell    string
	CaseSensitivity CaseSensitivity
	ShIsDash        bool
	Ephemeral       bool
}

type (
	Resolution    interface{ runnerResolution() }
	Resolved      struct{ Candidates []Candidate }
	NoMatch       struct{ Rejections []Rejection }
	Unknown       struct{ Reason string }
	NotApplicable struct{}
)

func (Resolved) runnerResolution()      {}
func (NoMatch) runnerResolution()       {}
func (Unknown) runnerResolution()       {}
func (NotApplicable) runnerResolution() {}

type DatasetStatus struct {
	DatasetID  string    `json:"dataset_id"`
	Scope      string    `json:"scope"`
	ObtainedAt string    `json:"obtained_at"`
	Freshness  Freshness `json:"freshness"`
	Complete   bool      `json:"complete"`
}
type Answer struct {
	Resolution    Resolution
	DatasetStatus DatasetStatus
}
type Descriptor struct {
	Name    string
	Version string
	Targets []string
}

// Provider returns one answer per selector, in request order. It must honor
// cancellation and support concurrent calls.
type Provider interface {
	RunnerDescriptor() Descriptor
	ResolveRunners(context.Context, Request) (Response, error)
}

type Registration struct {
	Provider Provider
	// Wasm loaders set its digest.
	ImplementationID string
}

type Options struct {
	Service   *services.Service
	AsOf      lifecycle.Date
	Target    string
	Provider  string
	Scope     string
	Snapshot  RunnerSnapshot
	Providers []Registration
}

// Report consumers caching dependent findings must include CacheKey in their cache key.
type Report struct {
	Service                                         services.Service
	AsOf                                            lifecycle.Date
	Support                                         SupportCatalog
	Path                                            string
	CacheKey                                        string
	Provider                                        Descriptor
	ImplementationID, Target, Scope, SnapshotDigest string
	Answers                                         []Answer
}
