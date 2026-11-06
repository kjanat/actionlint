// Package inputs defines shared check inputs and the checks that consume them, independently of AST traversal,
// executable tools, configuration discovery, and diagnostic rendering.
package inputs

import (
	"context"
	"encoding/json"
	"io/fs"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/source"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Descriptor declares identity and input requirements before execution.
type Descriptor struct {
	rulemeta.Descriptor
	Scope           rulemeta.ConfigurationScope
	SupportsActions bool
}

// Checker completes all work before returning. The host may call CheckDocument
// concurrently for different documents. Implementations must honor cancellation.
// Findings may accompany an error when an analysis is incomplete.
// Descriptor and CheckDocument must not panic; the host does not recover checker panics.
type Checker interface {
	Descriptor() Descriptor
	CheckDocument(context.Context, Input) ([]results.Diagnostic, error)
}

// Registration supplies analyzer-specific configuration without exposing the
// host's mutable configuration objects or rule registry.
type Registration struct {
	Checker Checker
	Options json.RawMessage
	// Prepare acquires additional facts before checking. On failure it returns
	// the usable partial input, so completed observations can still yield findings.
	Prepare func(context.Context, Input) (Input, error)
}

// Input is one document and its shared analysis facts. Each analyzer receives
// its own slices; Content and script text are immutable strings. Scripts are
// declared by the document itself. Invoked scripts belong to composite actions
// the document calls and name their own Path.
type Input struct {
	// Revision and Coordinates bind this request's own Path to captured bytes.
	// They are independent of the invocation graph's entry Reference.
	Revision    source.Revision
	Coordinates source.Coordinates
	Reference   source.Reference
	Path        string
	// Directory is the analysis root, not a filesystem grant.
	Directory       string
	Content         string
	Kind            string
	ParseStatus     string
	AvailableInputs []rulemeta.InputKind
	Options         json.RawMessage
	Scripts         []Script
	Invoked         []Script
	ActionInputs    []ActionInput
	CallGraph       CallGraph
	// SelectedScripts limits materialized text to these occurrences. Nil selects
	// every graph script; an empty non-nil slice selects none.
	SelectedScripts []OccurrenceID
	Mappings        []workflowfacts.Mapping
	Dependencies    []workflowfacts.Dependency
	// ResolvedDependencies contains successful, validated resolver observations.
	// Absence does not establish that an unresolved dependency is unavailable.
	ResolvedDependencies []workflowfacts.ResolvedDependency
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
// spans and optional occurrence identity; its Shell stays empty and Runtime stays nil.
type ActionInput struct {
	Script
	Site ScriptInput
}

// Script supplies a declaration and its invocation context to a check.
// An empty declaration Path means Input.Path. With the Invocations input,
// Occurrence identifies the execution in Input.CallGraph. Runtime is supplied
// with the Execution input.
type Script struct {
	scriptmodel.Declaration
	Occurrence ScriptOccurrence
	Runtime    *scriptmodel.Execution
}
