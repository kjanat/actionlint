package cli

import (
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev"
)

func parseColorMode(mode string) (actionlint.ColorOptionKind, error) {
	switch mode {
	case "auto":
		return actionlint.ColorOptionKindAuto, nil
	case "always":
		return actionlint.ColorOptionKindAlways, nil
	case "never":
		return actionlint.ColorOptionKindNever, nil
	default:
		return 0, fmt.Errorf("invalid color mode %q: choose auto, always or never", mode)
	}
}

func (a *commandApp) prepareColor() error {
	if a.opts.color {
		a.inv.Render.Color = actionlint.ColorOptionKindAlways
	}
	if a.set["modern-color"] {
		color, err := parseColorMode(a.opts.colorMode)
		if err != nil {
			return commandUsageError{err}
		}
		a.inv.Render.Color = color
	}
	if a.opts.noColor {
		a.inv.Render.Color = actionlint.ColorOptionKindNever
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

func (r *renderOptions) resolveGitHubActionsColor(out io.Writer) {
	if r.Color != actionlint.ColorOptionKindAuto || os.Getenv("GITHUB_ACTIONS") != "true" {
		return
	}
	r.Color = actionlint.ColorOptionKindNever
	if r.Template != "" || !githubActionsColor(out) {
		return
	}
	switch r.Format {
	case "", actionlint.OutputFormatText, actionlint.OutputFormatOneline:
		r.Color = actionlint.ColorOptionKindAlways
	}
}
