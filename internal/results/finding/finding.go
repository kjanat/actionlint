package finding

import (
	"cmp"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/binding"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results"
)

type Finding struct {
	results.Diagnostic
	Binding           binding.ID `clone:"shared"`
	LifecycleEvidence *lifecycle.Evidence
	InferredReference bool
	// Source is the text Start and End refer to when the finding points at a file other than the
	// linted workflow, and replaces the workflow source for rendering the snippet.
	Source            results.DiagnosticSource
	SuppressionOrigin *results.SuppressionOrigin
	// UnusedSuppressionRule identifies a candidate whose selector usage must be
	// reconciled across all invocations of the same captured source before publication.
	UnusedSuppressionRule string
	IgnoreOrigin          *workflows.IgnoreOrigin
	Site                  *Site
	MetadataIdentity      *MetadataIdentity
	MetadataParseSource   sourcetext.Revision
}

func (e *Finding) String() string {
	return fmt.Sprintf("%s:%d:%d: %s [%s]", e.Path, e.Start.Line, e.Start.Column, e.Message, e.Rule)
}

func At(pos *sourcetext.Position, kind, msg string) *Finding {
	return &Finding{
		Rule: kind, Message: msg,
		Start: *pos,
	}
}

func Errorf(pos *sourcetext.Position, kind, format string, args ...any) *Finding {
	return &Finding{
		Rule: kind, Message: fmt.Sprintf(format, args...),
		Start: *pos,
	}
}

// EndAt sets the exclusive end of the finding's range. A nil end keeps the
// range open, so rendering infers the token ending at Start.
func (e *Finding) EndAt(end *sourcetext.Position) {
	if end != nil {
		e.End = *end
	}
}

// Foreign reports whether the finding's positions refer to its own Source
// and none to source, the snapshot of the document it was reported for.
func (e *Finding) Foreign(source []byte) bool {
	return source != nil && e.Source.Content != nil
}

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
	return cmp.Compare(lhs.Severity, rhs.Severity)
}
