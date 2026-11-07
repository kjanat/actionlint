package output

import (
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/v2/internal/results"
)

// WriteGCC writes compiler-style diagnostics, one per line, without source
// excerpts or terminal styling. Information maps to GCC's note severity.
func WriteGCC(out io.Writer, diagnostics []results.Diagnostic) error {
	lineBreaks := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")
	for _, finding := range diagnostics {
		severity := "note"
		switch finding.Severity {
		case "", "error":
			severity = "error"
		case "warning":
			severity = "warning"
		}
		code := finding.Code
		if code == "" {
			code = finding.Rule
		}
		if _, err := fmt.Fprintf(out, "%s:%d:%d: %s: %s [%s]\n", finding.Path, finding.Start.Line, finding.Start.Column,
			severity, lineBreaks.Replace(finding.Message), code); err != nil {
			return err
		}
	}
	return nil
}
