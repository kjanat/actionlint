package finding

import (
	"fmt"
	"strings"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	bindingmodel "actionlint.kjanat.dev/v2/internal/model/binding"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

// Finding carries a canonical diagnostic and analysis-only provenance. All rule
// producers and plugins share Diagnostic; policy changes it in place.
type Finding struct {
	results.Diagnostic
	Binding           bindingmodel.ID `clone:"shared"`
	LifecycleEvidence *lifecycle.Evidence
	// inferredReference is traversal provenance, not a distinct analyzer finding.
	InferredReference bool
	// Source is the text Start and End refer to when the finding points at a file other than the
	// linted workflow, and replaces the workflow source for rendering the snippet.
	// Source.Suppression identifies YAML that an editor may amend to suppress this finding.
	Source results.DiagnosticSource
	// SuppressionOrigin identifies the YAML run declaration which caused a
	// sourced diagnostic, independently of its displayed script location.
	SuppressionOrigin *results.SuppressionOrigin
	// UnusedSuppressionRule identifies a candidate whose selector usage must be
	// reconciled across all invocations of the same captured source before publication.
	UnusedSuppressionRule string
	// ignoreOrigin identifies the logical invocation independently of anchor coordinates.
	IgnoreOrigin *IgnoreOrigin
	// owner is analysis provenance, populated independently of suppression settings.
	Owner *Owner
	// metadataFinding identifies a reusable validation finding independently of its caller.
	MetadataIdentity *MetadataIdentity
	// MetadataParseSource is the captured revision whose parse failure was
	// observed, bound by the producer before the finding is published.
	MetadataParseSource sourcepos.Revision
}

// String returns a diagnostic with its source location and rule name.
func (e *Finding) String() string {
	return fmt.Sprintf("%s:%d:%d: %s [%s]", e.Path, e.Start.Line, e.Start.Column, e.Message, e.Rule)
}

// At creates a finding at a workflow source position.
func At(pos *workflowlang.Pos, kind string, msg string) *Finding {
	return &Finding{
		Rule: kind, Message: msg,
		Start: sourcepos.Position{Line: pos.Line, Column: pos.Col},
	}
}

// Errorf formats a finding at a workflow source position.
func Errorf(pos *workflowlang.Pos, kind string, format string, args ...any) *Finding {
	return &Finding{
		Rule: kind, Message: fmt.Sprintf(format, args...),
		Start: sourcepos.Position{Line: pos.Line, Column: pos.Col},
	}
}

// EndAt sets the exclusive end of the finding's range. A nil end keeps the
// range open, so rendering infers the token ending at Start.
func (e *Finding) EndAt(end *workflowlang.Pos) {
	if end != nil {
		e.End = sourcepos.Position{Line: end.Line, Column: end.Col}
	}
}

// Foreign reports whether the finding's positions refer to its own Source
// instead of source, the snapshot of the document it was reported for. That
// is the case when a snapshot is available and the finding points at another
// file.
func (e *Finding) Foreign(source []byte) bool {
	return source != nil && e.Source.Content != nil
}

// sourceFor returns the text the finding's positions refer to: Source.Content
// when the finding is Foreign, otherwise source. Canonical only reads the
// snippet line from it.
func (e *Finding) sourceFor(source []byte) []byte {
	if e.Foreign(source) {
		return e.Source.Content
	}
	return source
}

// Compare orders findings by displayed location, message, extent and severity.
func Compare(lhs, rhs *Finding) int {
	if c := strings.Compare(lhs.Path, rhs.Path); c != 0 {
		return c
	}
	if lhs.Start.Line != rhs.Start.Line {
		return lhs.Start.Line - rhs.Start.Line
	}
	if lhs.Start.Column != rhs.Start.Column {
		return lhs.Start.Column - rhs.Start.Column
	}
	if c := strings.Compare(lhs.Message, rhs.Message); c != 0 {
		return c
	}
	if lhs.End.Line != rhs.End.Line {
		return lhs.End.Line - rhs.End.Line
	}
	if lhs.End.Column != rhs.End.Column {
		return lhs.End.Column - rhs.End.Column
	}
	return strings.Compare(lhs.Severity, rhs.Severity)
}
