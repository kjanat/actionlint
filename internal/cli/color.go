package cli

import (
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/render"
)

func parseColorMode(mode string) (render.Color, error) {
	switch mode {
	case "auto":
		return render.ColorAuto, nil
	case "always":
		return render.ColorAlways, nil
	case "never":
		return render.ColorNever, nil
	default:
		return 0, fmt.Errorf("invalid color mode %q: choose auto, always or never", mode)
	}
}

func (a *commandApp) prepareColor() error {
	if a.opts.color {
		a.inv.Render.Color = render.ColorAlways
	}
	if a.set["modern-color"] {
		color, err := parseColorMode(a.opts.colorMode)
		if err != nil {
			return commandUsageError{err}
		}
		a.inv.Render.Color = color
	}
	if a.opts.noColor {
		a.inv.Render.Color = render.ColorNever
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
	if r.Color != render.ColorAuto || os.Getenv("GITHUB_ACTIONS") != "true" {
		return
	}
	r.Color = render.ColorNever
	if r.Template != "" || !githubActionsColor(out) {
		return
	}
	switch r.Format {
	case "", render.FormatText, render.FormatOneline:
		r.Color = render.ColorAlways
	}
}
