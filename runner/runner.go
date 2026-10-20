// Package runner defines snapshot-based runner knowledge independently of a host,
// workflow parser, or WebAssembly runtime. Providers never emit diagnostics.
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"actionlint.kjanat.dev/v2/diagnostic"
)

// Term distinguishes literal labels from unresolved expressions.
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

// Constraint carries a host-owned source location; returned rejections must
// refer to one of the supplied constraints or use (0, 0) for the containing job.
type Constraint struct {
	Value    Term
	Position diagnostic.Position
}
type Rejection struct {
	Reason   string
	Position diagnostic.Position
}

// Selector is one conjunction. Separate selectors are alternatives, including
// matrix combinations. A nil Group means no group constraint.
type Selector struct {
	Incomplete bool
	Labels     []Constraint
	Group      *Constraint
	// Expression retains a whole selector that cannot be statically expanded.
	Expression string
}

type Freshness string

const (
	Fresh   Freshness = "fresh"
	Stale   Freshness = "stale"
	Missing Freshness = "missing"
)

// Snapshot is immutable evidence supplied explicitly by the host. Content is
// provider-specific data, never a capability to access files or the network.
type Snapshot struct {
	Content    string    `json:"content"`
	Scope      string    `json:"scope"`
	ObtainedAt string    `json:"obtained_at"`
	Freshness  Freshness `json:"freshness"`
	Complete   bool      `json:"complete"`
}

func (s Snapshot) Identity() string {
	data, _ := json.Marshal(s)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

type Request struct {
	Target    string
	Scope     string
	Snapshot  Snapshot
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

// Candidate describes explicit guarantees, not guesses from a label's spelling.
// Empty shell/architecture and unknown OS convey absent knowledge. False
// capabilities do not grant the associated execution assumption.
type Candidate struct {
	Name            string
	OS              OS
	Architecture    string
	DefaultShell    string
	CaseSensitivity CaseSensitivity
	ShIsDash        bool
	Ephemeral       bool
}

// Resolution separates semantic absence, uncertainty and non-applicability.
// Operational failures are Go errors, independent of these outcomes.
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

type Evidence struct {
	Dataset    string
	Scope      string
	ObtainedAt string
	Freshness  Freshness
	Complete   bool
}
type Answer struct {
	Resolution Resolution
	Evidence   Evidence
}
type Descriptor struct {
	Name    string
	Version string
	Targets []string
}

// Provider returns one answer per selector, in request order. It must honor
// cancellation and support concurrent calls. The host owns its lifetime.
type Provider interface {
	RunnerDescriptor() Descriptor
	ResolveRunners(context.Context, Request) ([]Answer, error)
}

type Registration struct {
	Provider Provider
	// Identity binds results to the implementation. Wasm loaders set its digest.
	Identity string
}

// Options selects authority explicitly. Empty target/provider selects the
// built-in GitHub provider; loading a component never changes that selection.
type Options struct {
	Target    string
	Provider  string
	Scope     string
	Snapshot  Snapshot
	Providers []Registration
}

// Report binds answers to the selected implementation, configuration and snapshot.
// Consumers caching dependent findings must include Identity in their cache key.
type Report struct {
	Path                              string
	Identity                          string
	Provider                          Descriptor
	Artifact, Target, Scope, Snapshot string
	Answers                           []Answer
}
