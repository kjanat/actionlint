// Package preparation builds scripts and options for linters. It replaces
// workflow expressions, determines language settings and adds directives or
// startup commands. Line maps relate the resulting text to the original script.
// Callers pass typed settings. This package does not load configuration files.
package preparation

import (
	"encoding/json"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
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
	AcceptDiagnostic func(scriptengine.Diagnostic) bool
	// Vars supplies values for placeholders in the tool command.
	Vars map[string]string
	// ConfigFile is the tool configuration file selected for this run. A
	// diagnostic the tool reports in it is a configuration error.
	ConfigFile string
	// Excluded lists diagnostic codes the tool should skip for this script.
	// Script engines receive these codes with each request.
	Excluded []string
	// Skip explains why the tool should not check this script.
	// An empty string allows checking.
	Skip string
	// Variants replace Source when templates have known values. Each variant
	// has the rows of Source and shares its LineMap.
	Variants []Variant
}

// Variant is Source with known values in place of masked templates.
type Variant struct {
	Source string
	// Substitutions are ordered by row and column.
	Substitutions []Substitution
}

// Substitution is a template that a variant replaces with one of its values.
type Substitution struct {
	Template string
	Value    string
	// Row and Column locate Template in the script. Column counts runes from 1.
	Row, Column int
	// Alternatives is the number of values the template can take.
	Alternatives int
}

// StartColumn maps a start column on a script row of the variant to the
// script. A start inside a value maps to the start of its template.
func (v Variant) StartColumn(row, column int) int {
	return v.scriptColumn(row, column, false)
}

// EndColumn maps an exclusive end column on a script row of the variant to
// the script. An end inside a value maps to the end of its template.
func (v Variant) EndColumn(row, column int) int {
	return v.scriptColumn(row, column, true)
}

func (v Variant) scriptColumn(row, column int, end bool) int {
	offset := 0
	for _, s := range v.Substitutions {
		if s.Row != row {
			continue
		}
		start := s.Column + offset
		if column < start || end && column == start {
			break
		}
		value, template := utf8.RuneCountInString(s.Value), utf8.RuneCountInString(s.Template)
		if column < start+value {
			if end {
				return s.Column + template
			}
			return s.Column
		}
		offset += value - template
	}
	return column - offset
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
