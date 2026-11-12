// Package output formats check results and writes approved source edits.
package output

import "actionlint.kjanat.dev/v2/internal/model/outputformat"

// Format selects a built-in diagnostic representation. An empty value selects
// output from the template and oneline options.
type Format string

const (
	FormatText    Format = outputformat.Text
	FormatPretty  Format = outputformat.Pretty
	FormatOneline Format = outputformat.Oneline
	FormatGCC     Format = outputformat.GCC
	FormatJSON    Format = outputformat.JSON
	FormatJSONL   Format = outputformat.JSONL
	FormatSARIF   Format = outputformat.SARIF
	FormatGitHub  Format = outputformat.GitHub
)
