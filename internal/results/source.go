package results

import "actionlint.kjanat.dev/v2/internal/base/sourcetext"

// SuppressionOrigin Lines locate the YAML run declaration that a suppression
// comment applies to, and an empty Path means the analyzed file.
type SuppressionOrigin struct {
	Path  string
	Lines sourcetext.LineRange
}

type DiagnosticSource struct {
	// Content is the file text positions refer to, when that file is not the
	// linted workflow itself.
	Content []byte `clone:"intern"`
	// RawScript selects LF-only line splitting for external checker positions,
	// which differ from YAML line breaks.
	RawScript bool
	// Unavailable forbids inferring ranges from unverified display text.
	Unavailable UnavailableSource
	// Suppression is present only when the resolved policy permits a comment
	// for this rule.
	Suppression *SuppressionOrigin
}
