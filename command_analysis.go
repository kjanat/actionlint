package actionlint

import "io"

func analyzeCommand(app *analysisApplication, stdin io.Reader, paths []string, normalizeStdin bool) (*AnalysisResult, error) {
	switch {
	case len(paths) == 0:
		return app.repository("")
	case len(paths) == 1 && paths[0] == "-":
		return app.readStdin(stdin, normalizeStdin)
	default:
		return app.files(paths, nil)
	}
}
