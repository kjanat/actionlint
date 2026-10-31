// Package profile defines what a preparation profile hands a script tool.
// Profiles are language preparation, not tools: they mask templates, infer
// the language dialect, insert directive and startup lines and map the
// prepared rows back to the script. They receive configuration as typed
// values and never import configuration loading.
package profile

import (
	"encoding/json"

	"actionlint.kjanat.dev/v2/analyzer"
)

// Prepared is a script ready for a tool. A nil LineMap leaves rows unchanged.
type Prepared struct {
	// TemplateContract is the template-effects contract the tool must declare
	// when Source masks templates; empty requires none.
	TemplateContract string
	Source           string
	Config           json.RawMessage
	// LineMap maps a row of Source to the script row it holds and to what
	// produced it.
	LineMap func(int) Line
	// AcceptDiagnostic filters findings preparation manufactured; nil keeps
	// every finding.
	AcceptDiagnostic func(analyzer.ScriptDiagnostic) bool
	// Vars are preparation variables for process argument placeholders.
	Vars map[string]string
	// Excluded are diagnostic codes the profile excludes, which snippet
	// engines receive with each request.
	Excluded []string
	// Skip is why preparation withholds the script from the tool. Empty
	// checks it.
	Skip string
}

// LineOrigin states what produced a row of prepared source.
type LineOrigin uint8

const (
	// LineScript is a row of the original script.
	LineScript LineOrigin = iota
	// LineDirective is a configuration directive preparation inserted. A
	// finding on it reports a configuration error.
	LineDirective
	// LineStartup is a runtime startup command preparation inserted. A
	// finding on it is dropped unless its severity is error.
	LineStartup
)

// Line is the script row a prepared row holds. Line is set only when Origin
// is LineScript.
type Line struct {
	Line   int
	Origin LineOrigin
}
