package sourcemap

import "actionlint.kjanat.dev/v2/internal/source/text"

func DiagnosticLine(source []byte, line int, rawScript bool) (string, bool) {
	if !rawScript {
		return SourceLine(string(source), line)
	}
	return text.LFLine(source, line)
}
