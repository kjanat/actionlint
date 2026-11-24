package output

import (
	"fmt"
	"io"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func diagnosticLabel(d report.DiagnosticCount) string {
	if d.Origin == results.DependencyOrigin {
		d.Origin = ""
		return "dependency:" + diagnosticLabel(d)
	}
	if d.Code == "" || d.Code == d.Rule {
		return d.Rule
	}
	if d.Rule == "" {
		return d.Code
	}
	return d.Rule + "/" + d.Code
}

func WriteSummaryDetails(out io.Writer, s report.CheckSummary) error {
	if len(s.IncompleteChecks) > 0 {
		if _, err := fmt.Fprintln(out, "\nIncomplete checks:"); err != nil {
			return err
		}
		for _, check := range s.IncompleteChecks {
			label := terminalText(check.Source.Document)
			if check.Rule != "" {
				label += " [" + terminalText(check.Rule) + "]"
			}
			if _, err := fmt.Fprintf(out, "  %s: %s\n", label, terminalText(check.Reason)); err != nil {
				return err
			}
		}
	}
	for _, group := range []struct {
		label string
		files []report.FileFindingCount
	}{{"Files", s.ByFile}, {"Dependencies", s.DependencyByFile}} {
		if len(group.files) == 0 {
			continue
		}
		width := 1
		for _, item := range group.files {
			width = max(width, len(strconv.Itoa(item.Count)))
		}
		if _, err := fmt.Fprintf(out, "\n%s:\n", group.label); err != nil {
			return err
		}
		for _, item := range group.files {
			if _, err := fmt.Fprintf(out, "  %*d  %s\n", width, item.Count, terminalText(item.Path)); err != nil {
				return err
			}
		}
	}
	if len(s.ByDiagnostic) > 0 {
		width := 1
		for _, item := range s.ByDiagnostic {
			width = max(width, len(strconv.Itoa(item.Count)))
		}
		if _, err := fmt.Fprintln(out, "\nDiagnostics:"); err != nil {
			return err
		}
		for _, item := range s.ByDiagnostic {
			if _, err := fmt.Fprintf(out, "  %*d  %s\n", width, item.Count, terminalText(diagnosticLabel(item))); err != nil {
				return err
			}
		}
	}
	return nil
}
