// Package output formats check results and writes approved source edits.
package output

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
