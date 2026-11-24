// Package preparation builds scripts and options for linters.
package preparation

import (
	"encoding/json"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

// Prepared is a script ready for a tool. A nil LineMap leaves rows unchanged.
type Prepared struct {
	TemplateContract string
	Source           string
	Config           json.RawMessage
	LineMap          func(int) Line
	// AcceptDiagnostic filters findings caused by generated script text.
	// A nil function accepts every finding.
	AcceptDiagnostic func(scriptengine.Diagnostic) bool
	Vars             map[string]string
	// ConfigFile is the tool configuration file selected for this run. A
	// diagnostic the tool reports in it is a configuration error.
	ConfigFile string
	Excluded   []string
	// Skip explains why the tool should not check this script.
	// An empty string allows checking.
	Skip string
	// Variants replace Source when templates have known values. Each variant
	// has the rows of Source and shares its LineMap.
	Variants []Variant
}

type Variant struct {
	Source string
	// Substitutions are ordered by row and column.
	Substitutions []Substitution
}

type Substitution struct {
	Template string
	Value    string
	// Row and Column locate Template in the script. Column counts runes from 1.
	Row, Column  int
	Alternatives int
}

func (v Variant) StartColumn(row, column int) int {
	return v.scriptColumn(row, column, false)
}

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

type LineOrigin uint8

const (
	LineScript LineOrigin = iota
	LineDirective
	LineStartup
)

type Line struct {
	Line   int
	Origin LineOrigin
}
