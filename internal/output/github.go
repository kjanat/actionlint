package output

import (
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/results"
)

// Match @actions/core workflow-command escaping. Property values additionally
// escape the separators between properties and between the header and body.
var (
	githubCommandData     = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	githubCommandProperty = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

// WriteGitHub writes one workflow command annotation per diagnostic.
func WriteGitHub(out io.Writer, diagnostics []results.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		endLine := diagnostic.End.Line
		if endLine > diagnostic.Start.Line && diagnostic.End.Column == 1 {
			endLine--
		}
		position := fmt.Sprintf("line=%d,endLine=%d", diagnostic.Start.Line, endLine)
		if diagnostic.Start.Line == diagnostic.End.Line {
			position += fmt.Sprintf(",col=%d,endColumn=%d", diagnostic.Start.Column, max(diagnostic.Start.Column, diagnostic.End.Column-1))
		}
		level := "error"
		switch diagnostic.Severity {
		case "warning":
			level = "warning"
		case "info", "note":
			level = "notice"
		}
		title := diagnostic.Code
		if title == "" {
			title = diagnostic.Rule
		}
		if _, err := fmt.Fprintf(out, "::%s file=%s,%s,title=%s::%s\n", level, githubCommandProperty.Replace(diagnostic.Path), position, githubCommandProperty.Replace(title), githubCommandData.Replace(diagnostic.Message)); err != nil {
			return err
		}
	}
	return nil
}
