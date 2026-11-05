package output

import (
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

func diagnosticLabel(d report.DiagnosticCount) string {
	if d.Code == "" || d.Code == d.Rule {
		return d.Rule
	}
	if d.Rule == "" {
		return d.Code
	}
	return d.Rule + "/" + d.Code
}

// WriteSummaryDetails writes compact counts without repeating diagnostic messages.
func WriteSummaryDetails(out io.Writer, s report.CheckSummary) error {
	if len(s.ByFile) > 0 {
		if _, err := fmt.Fprintln(out, "\nFiles:"); err != nil {
			return err
		}
		for _, item := range s.ByFile {
			if _, err := fmt.Fprintf(out, "  %d  %s\n", item.Count, terminalText(item.Path)); err != nil {
				return err
			}
		}
	}
	if len(s.ByDiagnostic) > 0 {
		if _, err := fmt.Fprintln(out, "\nDiagnostics:"); err != nil {
			return err
		}
		for _, item := range s.ByDiagnostic {
			if _, err := fmt.Fprintf(out, "  %d  %s\n", item.Count, terminalText(diagnosticLabel(item))); err != nil {
				return err
			}
		}
	}
	return nil
}
