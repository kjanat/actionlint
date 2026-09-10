package cli

import (
	"io"
	"os"

	"actionlint.kjanat.dev"
)

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
