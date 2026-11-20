package cli

import (
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/output"

	"actionlint.kjanat.dev/v2/internal/results/report"
	"github.com/fatih/color"
)

func writeCheckSummary(out io.Writer, mode output.Color, summary report.CheckSummary) error {
	files := "files"
	if summary.Files == 1 {
		files = "file"
	}
	text := fmt.Sprintf("✓ Checked %d %s. All clear!", summary.Files, files)
	attribute := color.FgGreen
	if summary.Files == 0 && summary.Findings == 0 {
		text = "No files checked."
		attribute = color.FgYellow
	}
	if summary.Findings > 0 {
		findings := "findings"
		if summary.Findings == 1 {
			findings = "finding"
		}
		text = fmt.Sprintf("! Checked %d %s, %d %s.", summary.Files, files, summary.Findings, findings)
		if summary.DependencyFindings > 0 {
			text = fmt.Sprintf("! Checked %d %s. Findings: %d project, %d dependency.", summary.Files, files, summary.ProjectFindings, summary.DependencyFindings)
		}
		attribute = color.FgYellow
	}
	if count := len(summary.IncompleteChecks); count > 0 {
		checks := "checks"
		if count == 1 {
			checks = "check"
		}
		text = fmt.Sprintf("! Checked %d %s. Findings: %d. %d %s could not finish.", summary.Files, files, summary.Findings, count, checks)
		if summary.DependencyFindings > 0 {
			text = fmt.Sprintf("! Checked %d %s. Findings: %d project, %d dependency. %d %s could not finish.", summary.Files, files, summary.ProjectFindings, summary.DependencyFindings, count, checks)
		}
		attribute = color.FgYellow
	}
	file, terminal := terminalFile(out)
	colored := mode == output.ColorAlways || mode == output.ColorAuto &&
		(githubActionsColor(out) || terminal && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb")
	style := color.New(attribute)
	if colored {
		style.EnableColor()
		if terminal {
			restore := enableTerminalVT(file)
			defer restore()
		}
	} else {
		style.DisableColor()
	}
	if _, err := fmt.Fprintln(out, style.Sprint(text)); err != nil {
		return err
	}
	return output.WriteSummaryDetails(out, summary)
}
