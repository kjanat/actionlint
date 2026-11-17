package cli

import (
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/model/outputformat"

	"actionlint.kjanat.dev/v2/internal/output"
)

func parseColorMode(mode string) (output.Color, error) {
	switch mode {
	case "auto":
		return output.ColorAuto, nil
	case "always":
		return output.ColorAlways, nil
	case "never":
		return output.ColorNever, nil
	default:
		return 0, fmt.Errorf("invalid color mode %q: choose auto, always or never", mode)
	}
}

func (a *commandApp) prepareColor() error {
	if a.opts.color {
		a.inv.Render.Color = output.ColorAlways
	}
	if a.set["modern-color"] {
		color, err := parseColorMode(a.opts.colorMode)
		if err != nil {
			return exitcode.UsageError{Cause: err}
		}
		a.inv.Render.Color = color
	}
	if a.opts.noColor {
		a.inv.Render.Color = output.ColorNever
	}
	return nil
}

func githubActionsColor(out io.Writer) bool {
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("NO_COLOR") != "" {
		return false
	}
	if file, ok := out.(*os.File); ok {
		info, err := file.Stat()
		if err != nil || info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func (r *renderOptions) resolveGitHubActionsColor(out io.Writer, format outputformat.Format) {
	if r.Color != output.ColorAuto || os.Getenv("GITHUB_ACTIONS") != "true" {
		return
	}
	r.Color = output.ColorNever
	if r.Template != "" || !githubActionsColor(out) {
		return
	}
	switch format {
	case "", outputformat.Text, outputformat.Pretty, outputformat.Oneline:
		r.Color = output.ColorAlways
	case outputformat.GCC, outputformat.JSON, outputformat.JSONL, outputformat.SARIF, outputformat.GitHub:
		// Machine-readable formats stay uncolored.
	}
}
