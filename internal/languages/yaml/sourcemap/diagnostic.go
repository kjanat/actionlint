package sourcemap

import "actionlint.kjanat.dev/v2/internal/input/sourcetext"

func DiagnosticLine(content []byte, line int, rawScript bool) (string, bool) {
	if !rawScript {
		return SourceLine(string(content), line)
	}
	return sourcetext.LFLine(content, line)
}
