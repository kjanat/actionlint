package engine

import (
	"fmt"
	"reflect"
	"strings"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Error carries a canonical diagnostic and analysis-only provenance. All rule
// producers and plugins share Diagnostic; policy changes it in place.
type Error struct {
	Diagnostic
	// inferredReference is traversal provenance, not a distinct analyzer finding.
	inferredReference bool
	// endColumn is the inclusive column where the error range ends. A zero value lets the formatter
	// infer the range from the source token at Column.
	endColumn   int
	endPosition *workflowlang.Pos
	// source is the content of the file at Filepath when the error points at a file other than the
	// linted workflow. It replaces the workflow source for rendering the snippet.
	source []byte
	// rawScriptSource retains the external checker's LF-based source coordinates.
	rawScriptSource bool
	// suppressionOrigin identifies the YAML run declaration which caused a
	// sourced diagnostic, independently of its displayed script location.
	suppressionOrigin *suppressionOrigin
	// ignoreOrigin identifies the logical invocation independently of anchor coordinates.
	ignoreOrigin *diagnosticIgnoreOrigin
	// owner is analysis provenance, populated independently of suppression settings.
	owner *diagnosticOwner
	// metadataFinding identifies a reusable validation finding independently of its caller.
	metadataFinding *actionMetadataFinding
	// metadataParsePath links caller parse failures to a selected manifest.
	metadataParsePath string
}

// Error returns summary of the error as string.
func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s [%s]", e.Path, e.Start.Line, e.Start.Column, e.Message, e.Rule)
}

func (e *Error) String() string {
	return e.Error()
}

func errorAt(pos *workflowlang.Pos, kind string, msg string) *Error {
	return &Error{
		Message: msg, Start: DiagnosticPosition{Line: pos.Line,
			Column: pos.Col}, Rule: kind,
	}
}

func errorfAt(pos *workflowlang.Pos, kind string, format string, args ...any) *Error {
	return &Error{
		Message: fmt.Sprintf(format, args...), Start: DiagnosticPosition{Line: pos.Line,
			Column: pos.Col}, Rule: kind,
	}
}

func (e *Error) sourceFor(source []byte) []byte {
	if source != nil && e.source != nil {
		return e.source
	}
	return source
}

func compareErrors(lhs, rhs *Error) int {
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
	if lhs.endColumn != rhs.endColumn {
		return lhs.endColumn - rhs.endColumn
	}
	return strings.Compare(lhs.Severity, rhs.Severity)
}

func equalsErrors(lhs, rhs *Error) bool {
	left, right := lhs.Diagnostic, rhs.Diagnostic
	// Aliases can reach one declaration through different invocation owners.
	// Explicit analyzer references remain part of diagnostic identity.
	if lhs.inferredReference && rhs.inferredReference {
		left.Reference, right.Reference = nil, nil
	}
	return reflect.DeepEqual(left, right) &&
		lhs.endColumn == rhs.endColumn && reflect.DeepEqual(lhs.endPosition, rhs.endPosition)
}
