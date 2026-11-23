// Package checkinputs defines shared check inputs.
package checkinputs

import (
	"encoding/json"
	"io/fs"

	"actionlint.kjanat.dev/v2/internal/model/commands"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

type Descriptor struct {
	rule.Declaration
	InvocationDefinitions []commands.Definition
}

// AnalyzerInput is one document and its shared analysis facts. Each analyzer
// receives its own slices.
type AnalyzerInput struct {
	Revision    sourcetext.Revision
	Coordinates sourcetext.Coordinates
	Reference   sourcetext.Reference
	Path        string
	// Directory names the analysis root. Filesystem access requires a separate grant.
	Directory       string
	Content         string
	Kind            string
	ParseStatus     string
	AvailableInputs []rule.InputKind
	Options         json.RawMessage
	Scripts         []Script
	Invoked         []Script
	ActionInputs    []ActionInput
	CallGraph       invocation.Graph `clone:"shared"`
	// SelectedScripts limits materialized text to these occurrences. Nil selects
	// every graph script; an empty non-nil slice selects none.
	SelectedScripts []invocation.ID
	Mappings        []facts.Mapping
	Dependencies    []facts.Dependency
	// Absence does not establish that an unresolved dependency is unavailable.
	ResolvedDependencies []facts.ResolvedDependency
	// Files grants read-only filesystems for this call. Nil grants no file access.
	Files                 []FileSystem
	SourceRoots           []SourceRoot
	BuildShellScriptInput ShellScriptInputBuilder `clone:"shared"`
}

type FileSystem struct {
	Path string
	FS   fs.FS `clone:"shared"`
}

type ActionInput struct {
	Script
	Site ScriptInput
}

// Script has an empty declaration Path when it belongs to AnalyzerInput.Path.
type Script struct {
	scripts.Declaration
	Occurrence   invocation.Script
	Runtime      *scripts.Execution
	Environment  []scripts.EnvironmentNames
	Interpreters *commands.Registry `clone:"shared"`
}
