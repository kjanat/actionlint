package cli

import (
	"io"

	"actionlint.kjanat.dev"
)

func analyzeCommand(app *actionlint.AnalysisSession, stdin io.Reader, paths []string, normalizeStdin bool) (*actionlint.AnalysisResult, error) {
	switch {
	case len(paths) == 0:
		return app.Repository("")
	case len(paths) == 1 && paths[0] == "-":
		return app.ReadStdin(stdin, normalizeStdin)
	default:
		return app.Files(paths, nil)
	}
}
