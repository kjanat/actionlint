// Package preparation builds scripts and options for linters. It replaces
// workflow expressions, determines language settings and adds directives or
// startup commands. Line maps relate the resulting text to the original script.
// Callers pass typed settings. This package does not load configuration files.
package preparation

import (
	"encoding/json"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
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
	// AcceptDiagnostic filters findings caused by generated script text.
	// A nil function accepts every finding.
	AcceptDiagnostic func(checkinputs.ScriptDiagnostic) bool
	// Vars supplies values for placeholders in the tool command.
	Vars map[string]string
	// Excluded lists diagnostic codes the tool should skip for this script.
	// Script engines receive these codes with each request.
	Excluded []string
	// Skip explains why the tool should not check this script.
	// An empty string allows checking.
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

// Line identifies the origin of a prepared row. Line is set only when Origin
// is LineScript and gives the corresponding row in the original script.
type Line struct {
	Line   int
	Origin LineOrigin
}
