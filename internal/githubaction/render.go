package githubaction

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"actionlint.kjanat.dev"
)

func commandEscape(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(value)
}

// These compatibility renderers consume the typed analysis directly. The shared
// formatter retains source-aware caret widths and inclusive legacy columns.
const actionHeader = `{{define "header"}}{{if eq (printf "%.2s" .Filepath) "::"}}./{{end}}{{.Filepath}}:{{.Line}}:{{.Column}}: {{.Message}} [{{.Kind}}]{{end}}`

const actionDefault = actionHeader + `{{range .}}{{template "header" .}}
{{if .Snippet}}{{$indent := printf "%*s " (len (printf "%d" .Line)) ""}}{{$indent}}|
{{.Line}} | {{replace .Snippet (printf "%c" 10) (printf "%c%s| " 10 $indent)}}
{{end}}{{end}}`

const actionOneline = actionHeader + `{{range .}}{{template "header" .}}
{{end}}`

const actionMarkdown = `{{range .}}### {{.Filepath}}:{{.Line}}:{{.Column}} ({{.Kind}})

{{.Message}}
{{if .Snippet}}
    {{replace .Snippet (printf "%c" 10) (printf "%c    " 10)}}
{{end}}
{{end}}`

const actionGitHub = `{{define "data"}}{{replace . "%" "%25" (printf "%c" 13) "%0D" (printf "%c" 10) "%0A"}}{{end}}{{define "property"}}{{replace . "%" "%25" (printf "%c" 13) "%0D" (printf "%c" 10) "%0A" ":" "%3A" "," "%2C"}}{{end}}{{range .}}::error file={{template "property" .Filepath}},line={{.Line}},col={{.Column}},endColumn={{.EndColumn}},title={{template "property" (printf "actionlint (%s)" .Kind)}}::{{template "data" .Message}}{{if .Snippet}}%0A%0A{{template "data" .Snippet}}{{end}}
{{end}}`

func renderAnalysis(format outputFormat, analysis *actionlint.AnalysisResult, workingDir, workspaceDir string) (string, error) {
	var template string
	switch format {
	case formatDefault:
		template = actionDefault
	case formatOneline:
		template = actionOneline
	case formatMarkdown:
		template = actionMarkdown
	case formatGitHub:
		template = actionGitHub
		// Only annotation serialization uses workspace-relative paths.
		analysis = workspaceReportAnalysis(analysis, workingDir, workspaceDir)
	default:
		return "", fmt.Errorf("format %q has no compatibility renderer", format)
	}
	renderer, err := actionlint.NewAnalysisRenderer("", template, false)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := renderer.Render(&output, analysis); err != nil {
		return "", err
	}
	return output.String(), nil
}

func renderOutcome(result *lintResult) (*lintOutcome, string, string) {
	o := result.lintOutcome
	if o.code != actionlint.ExitStatusSuccessNoProblem && o.code != actionlint.ExitStatusSuccessProblemFound {
		return o, "", o.stderr + o.stdout
	}
	return o, strconv.Itoa(len(result.diagnostics)), o.stdout
}
