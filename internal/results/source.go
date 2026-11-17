package results

import "actionlint.kjanat.dev/v2/internal/base/sourcetext"

// SuppressionOrigin identifies the YAML run declaration, as the Lines of
// Path, that a suppression comment applies to. An empty Path means the
// analyzed file itself.
type SuppressionOrigin struct {
	Path  string
	Lines sourcetext.LineRange
}

// DiagnosticSource is the source text a diagnostic is rendered from. It
// supplies source coordinates without imposing a display format.
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
	// for this rule. Editors bind its YAML target to a captured revision.
	Suppression *SuppressionOrigin
}
