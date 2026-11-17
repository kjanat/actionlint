package lsp

import "actionlint.kjanat.dev/v2/internal/results"

// reportedSourceIf returns the marker of a reported source whose text is
// unavailable, or the zero marker for an available one.
func reportedSourceIf(unavailable bool) results.UnavailableSource {
	var marker results.UnavailableSource
	if unavailable {
		marker = results.ReportedSourceUnavailable("tool", "reported.sh", false)
	}
	return marker
}
