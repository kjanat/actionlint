// Package analyzer defines document analyzers independently of AST traversal,
// executable tools, configuration discovery, and diagnostic rendering.
package analyzer

import (
	"context"
	"encoding/json"

	"actionlint.kjanat.dev/diagnostic"
)

// Fact identifies shared information requested by an analyzer.
type Fact string

const (
	Source   Fact = "source"
	Scripts  Fact = "scripts"
	Mappings Fact = "mappings"
)

// Descriptor declares identity and input requirements before execution.
type Descriptor struct {
	Name     string
	Requires []Fact
}

// Analyzer completes all work before returning. The host may call Analyze
// concurrently for different documents. Implementations must honor cancellation.
// Findings may accompany an error when an analysis is incomplete.
type Analyzer interface {
	Descriptor() Descriptor
	Analyze(context.Context, Input) ([]diagnostic.Diagnostic, error)
}

// Registration supplies analyzer-specific configuration without exposing the
// host's mutable configuration objects or rule registry.
type Registration struct {
	Analyzer Analyzer
	Options  json.RawMessage
}

// Input is one document and its shared analysis facts. Each analyzer receives
// its own slices; Content and script text are immutable strings.
type Input struct {
	Path        string
	Content     string
	Kind        string
	ParseStatus string
	Available   []Fact
	Options     json.RawMessage
	Scripts     []Script
	Mappings    []Mapping
}

// Script is a declared run script. Shell contains the declared shell, without
// resolving runner defaults. Start points to the original run value.
type Script struct {
	JobID  string
	StepID string
	Text   string
	Shell  string
	Start  diagnostic.Position
}

// Mapping contains normalized names and source provenance for a declaration.
// Complete is false when an insertion or key depends on runtime values.
type Mapping struct {
	Scope    string
	Complete bool
	Entries  []Entry
}

// Entry separates an evaluated name from source expression text. Evaluated
// entries came from object expressions and must not be interpreted again.
type Entry struct {
	Name      string
	State     KeyState
	Source    string
	Evaluated bool
	Start     diagnostic.Position
}

// KeyState distinguishes a runtime name from a statically invalid declaration.
type KeyState string

const (
	UnknownKey KeyState = "unknown"
	KnownKey   KeyState = "known"
	InvalidKey KeyState = "invalid"
)
