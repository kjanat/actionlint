package finding

import (
	"fmt"
	"reflect"
	"strings"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

// Finding carries a canonical diagnostic and analysis-only provenance. All rule
// producers and plugins share Diagnostic; policy changes it in place.
type Finding struct {
	diagnostic.Diagnostic
	// inferredReference is traversal provenance, not a distinct analyzer finding.
	InferredReference bool
	// endColumn is the inclusive column where the error range ends. A zero value lets the formatter
	// infer the range from the source token at Column.
	EndColumn   int
	EndPosition *workflowlang.Pos
	// source is the content of the file at Filepath when the error points at a file other than the
	// linted workflow. It replaces the workflow source for rendering the snippet.
	Source []byte
	// RawScriptSource selects LF-only line splitting for external checker positions.
	RawScriptSource bool
	// suppressionOrigin identifies the YAML run declaration which caused a
	// sourced diagnostic, independently of its displayed script location.
	SuppressionOrigin *SuppressionOrigin
	// ignoreOrigin identifies the logical invocation independently of anchor coordinates.
	IgnoreOrigin *IgnoreOrigin
	// owner is analysis provenance, populated independently of suppression settings.
	Owner *Owner
	// metadataFinding identifies a reusable validation finding independently of its caller.
	MetadataIdentity *MetadataIdentity
	// metadataParsePath links caller parse failures to a selected manifest.
	MetadataParsePath string
}

// String returns a diagnostic with its source location and rule name.
func (e *Finding) String() string {
	return fmt.Sprintf("%s:%d:%d: %s [%s]", e.Path, e.Start.Line, e.Start.Column, e.Message, e.Rule)
}

// At creates a finding at a workflow source position.
func At(pos *workflowlang.Pos, kind string, msg string) *Finding {
	return &Finding{
		Rule: kind, Message: msg,
		Start: diagnostic.Position{Line: pos.Line, Column: pos.Col},
	}
}

// Errorf formats a finding at a workflow source position.
func Errorf(pos *workflowlang.Pos, kind string, format string, args ...any) *Finding {
	return &Finding{
		Rule: kind, Message: fmt.Sprintf(format, args...),
		Start: diagnostic.Position{Line: pos.Line, Column: pos.Col},
	}
}

// SourceFor selects the finding's foreign source when a source snapshot is available.
func (e *Finding) SourceFor(source []byte) []byte {
	if source != nil && e.Source != nil {
		return e.Source
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
	if lhs.EndColumn != rhs.EndColumn {
		return lhs.EndColumn - rhs.EndColumn
	}
	return strings.Compare(lhs.Severity, rhs.Severity)
}

// Equal compares diagnostic identity while ignoring traversal-inferred reference differences.
func Equal(lhs, rhs *Finding) bool {
	left, right := lhs.Diagnostic, rhs.Diagnostic
	// Aliases can reach one declaration through different invocation owners.
	if lhs.InferredReference && rhs.InferredReference {
		left.Reference, right.Reference = nil, nil
	}
	return reflect.DeepEqual(left, right) &&
		lhs.EndColumn == rhs.EndColumn && reflect.DeepEqual(lhs.EndPosition, rhs.EndPosition)
}
