package output

import (
	"io"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Match @actions/core workflow-command escaping. Property values additionally
// escape the separators between properties and between the header and body.
var (
	githubCommandData     = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	githubCommandProperty = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

// GitHubCommandProperty is one name=value property of a workflow command.
type GitHubCommandProperty struct{ Name, Value string }

// FormatGitHubCommand formats one workflow command line, such as
// "::error file=a.yml,line=1,title=t::message\n". It escapes property values
// and the message the way @actions/core does, so neither can end the command
// early or start another one.
func FormatGitHubCommand(command string, properties []GitHubCommandProperty, message string) string {
	var line strings.Builder
	line.WriteString("::")
	line.WriteString(command)
	for i, property := range properties {
		if i == 0 {
			line.WriteByte(' ')
		} else {
			line.WriteByte(',')
		}
		line.WriteString(property.Name)
		line.WriteByte('=')
		line.WriteString(githubCommandProperty.Replace(property.Value))
	}
	line.WriteString("::")
	line.WriteString(githubCommandData.Replace(message))
	line.WriteByte('\n')
	return line.String()
}

// EscapeGitHubCommandData escapes text written to a stream the runner scans
// for workflow commands, so the text stays on one line.
func EscapeGitHubCommandData(text string) string { return githubCommandData.Replace(text) }

// GitHubAnnotationCommand returns the workflow command that annotates a
// finding of the given severity: notice, warning or error.
func GitHubAnnotationCommand(level severity.Severity) string {
	switch level.Effective() {
	case severity.Warning:
		return "warning"
	case severity.Info:
		return "notice"
	case severity.Error:
	}
	return "error"
}

// WriteGitHub writes one workflow command annotation per diagnostic.
func WriteGitHub(out io.Writer, diagnostics []results.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		endLine := diagnostic.End.Line
		if endLine > diagnostic.Start.Line && diagnostic.End.Column == 1 {
			endLine--
		}
		properties := []GitHubCommandProperty{
			{"file", diagnostic.Path},
			{"line", strconv.Itoa(diagnostic.Start.Line)},
			{"endLine", strconv.Itoa(endLine)},
		}
		if diagnostic.Start.Line == diagnostic.End.Line {
			properties = append(properties,
				GitHubCommandProperty{"col", strconv.Itoa(diagnostic.Start.Column)},
				GitHubCommandProperty{"endColumn", strconv.Itoa(max(diagnostic.Start.Column, diagnostic.End.Column-1))},
			)
		}
		title := diagnostic.Code
		if title == "" {
			title = diagnostic.Rule
		}
		if diagnostic.Origin == results.DependencyOrigin {
			title = "dependency:" + title
		}
		properties = append(properties, GitHubCommandProperty{"title", title})
		if _, err := io.WriteString(out, FormatGitHubCommand(GitHubAnnotationCommand(diagnostic.Severity), properties, diagnostic.Message)); err != nil {
			return err
		}
	}
	return nil
}
