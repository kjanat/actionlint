package lsp

import "actionlint.kjanat.dev/v2/internal/results"

func reportedSourceIf(unavailable bool) results.UnavailableSource {
	var marker results.UnavailableSource
	if unavailable {
		marker = results.ReportedSourceUnavailable("tool", "reported.sh", false)
	}
	return marker
}
