// Package output formats check results and writes approved source edits.
package output

import "slices"

// Format selects a built-in diagnostic representation. An empty value selects
// output from the template and oneline options.
type Format string

const (
	FormatText    Format = "text"
	FormatPretty  Format = "pretty"
	FormatOneline Format = "oneline"
	FormatGCC     Format = "gcc"
	FormatJSON    Format = "json"
	FormatJSONL   Format = "jsonl"
	FormatSARIF   Format = "sarif"
	FormatGitHub  Format = "github"
)

var formatNames = []string{
	string(FormatText),
	string(FormatPretty),
	string(FormatOneline),
	string(FormatGCC),
	string(FormatJSON),
	string(FormatJSONL),
	string(FormatSARIF),
	string(FormatGitHub),
}

// FormatNames returns the supported output formats in display order.
func FormatNames() []string { return slices.Clone(formatNames) }

// ValidFormat reports whether name is a supported output format.
func ValidFormat(name string) bool { return slices.Contains(formatNames, name) }
