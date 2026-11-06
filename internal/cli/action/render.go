package githubaction

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking"
	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	render "actionlint.kjanat.dev/v2/internal/output"
)

func commandEscape(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(value)
}

const actionHeader = `{{define "header"}}{{if eq (printf "%.2s" .Filepath) "::"}}./{{end}}{{.Filepath}}:{{.Line}}:{{.Column}}: {{if and .Code (ne .Code .Kind)}}{{.Code}}: {{end}}{{.Message}} [{{.Kind}}]{{end}}`

const actionDefault = actionHeader + `{{range .}}{{template "header" .}}
{{if .Snippet}}{{$indent := printf "%*s " (len (printf "%d" .Line)) ""}}{{$indent}}|
{{.Line}} | {{replace .Snippet (printf "%c" 10) (printf "%c%s| " 10 $indent)}}
{{end}}{{end}}`

const actionOneline = actionHeader + `{{range .}}{{template "header" .}}
{{end}}`

const actionMarkdown = `{{range .}}### {{.Filepath}}:{{.Line}}:{{.Column}} ({{.Kind}}{{if and .Code (ne .Code .Kind)}}/{{.Code}}{{end}})

{{.Message}}
{{if .Snippet}}
    {{replace .Snippet (printf "%c" 10) (printf "%c    " 10)}}
{{end}}
{{end}}`

const actionGitHub = `{{range .}}::{{if eq .Severity "info"}}notice{{else if eq .Severity "warning"}}warning{{else}}error{{end}} file={{escapeProperty .Filepath}},line={{.Line}},col={{.Column}},endColumn={{.EndColumn}},title=actionlint ({{escapeProperty .Kind}}{{if and .Code (ne .Code .Kind)}}/{{escapeProperty .Code}}{{end}})::{{escapeData .Message}}{{if .Snippet}}%0A%0A{{escapeData .Snippet}}{{end}}
{{end}}`

func renderAnalysis(format outputFormat, analysis *checking.Result, workingDir, workspaceDir string) (string, error) {
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
	case formatJSON, formatJSONLines, formatSARIF:
		// Structured formats are serialized without templates.
		fallthrough
	default:
		return "", fmt.Errorf("format %q has no compatibility renderer", format)
	}
	renderer, err := render.New("", template, false, buildinfo.Version, nil)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := renderer.Render(&output, render.FromAnalysis(*analysis)); err != nil {
		return "", err
	}
	return output.String(), nil
}

func renderOutcome(result *lintResult) (*lintOutcome, string, string) {
	o := result.lintOutcome
	if o.code != commandstatus.ExitSuccessNoProblem && o.code != commandstatus.ExitSuccessProblemFound {
		return o, "", o.stderr + o.stdout
	}
	return o, strconv.Itoa(len(result.report.Diagnostics)), o.stdout
}
