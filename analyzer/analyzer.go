// Package analyzer defines document analyzers independently of AST traversal,
// executable tools, configuration discovery, and diagnostic rendering.
package analyzer

import (
	"context"
	"encoding/json"
	"io/fs"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/rule"
	"actionlint.kjanat.dev/v2/source"
)

// Fact identifies shared information requested by an analyzer.
type Fact string

const (
	Source       Fact = "source"
	Scripts      Fact = "scripts"
	Mappings     Fact = "mappings"
	Invocations  Fact = "invocations"
	Execution    Fact = "execution"
	Dependencies Fact = "dependencies"
)

// Descriptor declares identity and input requirements before execution.
type Descriptor struct {
	rule.Descriptor
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
// its own slices; Content and script text are immutable strings. Scripts are
// declared by the document itself. Invoked scripts belong to composite actions
// the document calls and name their own Path.
type Input struct {
	Reference    source.Reference
	Path         string
	Content      string
	Kind         string
	ParseStatus  string
	Available    []Fact
	Options      json.RawMessage
	Scripts      []Script
	Invoked      []Script
	ActionInputs []ActionInput
	CallGraph    CallGraph
	// SelectedScripts limits materialized text to these occurrences. Nil selects
	// every graph script; an empty non-nil slice selects none.
	SelectedScripts []OccurrenceID
	Mappings        []Mapping
	Dependencies    []Dependency
	// Files grants read-only filesystems for this call. Nil grants no file access.
	// Mount paths are absolute guest paths; filesystem directories must implement
	// descriptor-relative read operations when used by a component host.
	Files         []FileSystem
	SourceRoots   []SourceRoot
	PrepareScript ScriptPreparer
}

// FileSystem mounts a read-only filesystem at a guest path for one Analyze call.
// The caller must keep FS open until Analyze returns.
type FileSystem struct {
	Path string
	FS   fs.FS
}

// ActionInput is mapped source supplied to an action rather than a run command.
// Site declares its language and action input. Script supplies source identity,
// spans and invocation only; its Shell stays empty and Runtime stays nil.
type ActionInput struct {
	Script
	Site ScriptInput
}

// Script is a declared run script. Shell contains the declared shell, without
// resolving runner defaults. Start points to the original run value in the
// document at Path; an empty Path means Input.Path. Spans map text positions
// back to that document. Invocation is set for scripts reached through a
// composite action call. Runtime is set when the Execution fact was requested.
type Script struct {
	Reference  source.Reference
	Occurrence ScriptOccurrence
	JobID      string
	StepID     string
	Text       string
	Shell      string
	Start      diagnostic.Position
	Path       string
	Spans      []Span
	Invocation *Invocation
	Runtime    *Runtime
	// Comments supplies YAML comments through the optional source-annotations interface.
	Comments []SourceComment
}

// CommentPlacement identifies a YAML comment's attachment to a declaration.
type CommentPlacement uint8

const (
	LeadingComment CommentPlacement = iota
	TrailingComment
)

// SourceComment is one physical YAML comment, including '#'. Start points to '#'.
type SourceComment struct {
	Text      string
	Start     diagnostic.Position
	Placement CommentPlacement
}

// Span maps the text columns [Start, End) of one line of Text to the declaring
// document. Document is the position of the first mapped character. DocumentEnd
// is nonzero for a decoded escape sequence, whose source width differs from its
// text width.
type Span struct {
	Line        int
	Start       int
	End         int
	Document    diagnostic.Position
	DocumentEnd diagnostic.Position
}

// Invocation identifies the step whose uses: reference reached a composite
// action's script. Caller names the document containing that step; an empty
// Caller means Input.Path. Depth counts composite calls from Input.Path.
type Invocation struct {
	Reference source.Reference
	Caller    string
	Step      diagnostic.Position
	Uses      string
	Depth     int
}

// Runtime carries the shell and working directory the runner would use.
// ShellKnown and DirectoryKnown report whether static analysis determined the
// value. ShellImplicit marks a shell supplied by the runner or platform.
type Runtime struct {
	Shell          string
	ShellKnown     bool
	ShellImplicit  bool
	Directory      string
	DirectoryKnown bool
}

// Position maps a one-based line and character column of Text to the declaring
// document. Exclusive selects the end of a range. Text without a source, such as
// whitespace synthesized by YAML folding, reports false.
func (s Script) Position(line, column int, exclusive bool) (diagnostic.Position, bool) {
	for _, span := range s.Spans {
		if span.Line != line {
			continue
		}
		inside := column >= span.Start && column < span.End
		if exclusive {
			inside = column > span.Start && column <= span.End || span.Start == span.End && column == span.Start
		}
		if !inside {
			continue
		}
		if span.DocumentEnd.Line != 0 {
			if exclusive && column == span.End {
				return span.DocumentEnd, true
			}
			return span.Document, true
		}
		return diagnostic.Position{Line: span.Document.Line, Column: span.Document.Column + column - span.Start}, true
	}
	return diagnostic.Position{}, false
}

// Mapping contains normalized names and source provenance for a declaration.
// Complete is false when an insertion or key depends on runtime values.
type Mapping struct {
	Reference source.Reference
	Scope     string
	Complete  bool
	Entries   []Entry
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

// Dependency is a statically known action or reusable-workflow reference.
// Its range is the complete YAML scalar, including quotes when present.
type Dependency struct {
	Reference  source.Reference
	Uses       string
	Kind       string
	Start, End diagnostic.Position
}
