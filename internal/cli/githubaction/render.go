package githubaction

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	"actionlint.kjanat.dev/v2/internal/output"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

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

func renderAnalysis(format outputFormat, analysis *report.Analysis, workingDir, workspaceDir string) (string, error) {
	var template string
	switch format {
	case formatDefault:
		template = actionDefault
	case formatOneline:
		template = actionOneline
	case formatMarkdown:
		template = actionMarkdown
	case formatGitHub:
		return actionAnnotations(workspaceReportAnalysis(analysis, workingDir, workspaceDir)), nil
	case formatJSON, formatJSONLines, formatSARIF:
		fallthrough
	default:
		return "", fmt.Errorf("format %q has no compatibility renderer", format)
	}
	renderer, err := output.New("", template, false, buildinfo.Version, nil)
	if err != nil {
		return "", err
	}
	var rendered bytes.Buffer
	if err := renderer.Render(&rendered, output.FromAnalysis(*analysis)); err != nil {
		return "", err
	}
	return rendered.String(), nil
}

func actionAnnotations(analysis *report.Analysis) string {
	var annotations strings.Builder
	for _, finding := range analysis.Diagnostics {
		fields := output.TemplateFieldsOf(finding)
		title := "actionlint ("
		if finding.Origin == results.DependencyOrigin {
			title += "dependency:"
		}
		title += fields.Kind
		if fields.Code != "" && fields.Code != fields.Kind {
			title += "/" + fields.Code
		}
		title += ")"
		message := fields.Message
		if fields.Snippet != "" {
			message += "\n\n" + fields.Snippet
		}
		annotations.WriteString(output.FormatGitHubCommand(output.GitHubAnnotationCommand(finding.Severity), []output.GitHubCommandProperty{
			{Name: "file", Value: fields.Filepath},
			{Name: "line", Value: strconv.Itoa(fields.Line)},
			{Name: "col", Value: strconv.Itoa(fields.Column)},
			{Name: "endColumn", Value: strconv.Itoa(fields.EndColumn)},
			{Name: "title", Value: title},
		}, message))
	}
	return annotations.String()
}

func renderOutcome(result *lintResult) (*lintOutcome, string, string) {
	o := result.lintOutcome
	if o.code != exitcode.ExitSuccessNoProblem && o.code != exitcode.ExitSuccessProblemFound {
		return o, "", o.stderr + o.stdout
	}
	return o, strconv.Itoa(len(result.report.Diagnostics)), o.stdout
}
