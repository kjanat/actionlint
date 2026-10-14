package cli

import (
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev"
	"github.com/fatih/color"
)

func writeCheckSummary(out io.Writer, mode actionlint.ColorOptionKind, summary actionlint.CheckSummary) error {
	workflows := "workflows"
	if summary.Files == 1 {
		workflows = "workflow"
	}
	text := fmt.Sprintf("✓ Checked %d %s. All clear!", summary.Files, workflows)
	attribute := color.FgGreen
	if summary.Findings > 0 {
		findings := "findings"
		if summary.Findings == 1 {
			findings = "finding"
		}
		text = fmt.Sprintf("! Checked %d %s, %d %s.", summary.Files, workflows, summary.Findings, findings)
		attribute = color.FgYellow
	}
	file, terminal := terminalFile(out)
	colored := mode == actionlint.ColorOptionKindAlways || mode == actionlint.ColorOptionKindAuto &&
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
	_, err := fmt.Fprintln(out, style.Sprint(text))
	return err
}
