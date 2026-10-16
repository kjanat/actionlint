package engine

import (
	"fmt"
	"io"
	"strings"

	"actionlint.kjanat.dev/internal/render"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// Error represents an error detected by actionlint rules
type Error struct {
	// Message is an error message.
	Message string
	// Filepath is a file path where the error occurred.
	Filepath string
	// Line is a line number where the error occurred. This value is 1-based.
	Line int
	// Column is a column number where the error occurred. This value is 1-based.
	Column int
	// Kind is a string to represent kind of the error. Usually rule name which found the error.
	Kind string
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
	// External analyzer metadata is exposed through Diagnostic, leaving legacy output unchanged.
	code     string
	severity string
	fixes    []DiagnosticFix
	details  *Diagnostic
}

// Error returns summary of the error as string.
func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s [%s]", e.Filepath, e.Line, e.Column, e.Message, e.Kind)
}

func (e *Error) String() string {
	return e.Error()
}

func errorAt(pos *workflowlang.Pos, kind string, msg string) *Error {
	return &Error{
		Message: msg,
		Line:    pos.Line,
		Column:  pos.Col,
		Kind:    kind,
	}
}

func errorfAt(pos *workflowlang.Pos, kind string, format string, args ...any) *Error {
	return &Error{
		Message: fmt.Sprintf(format, args...),
		Line:    pos.Line,
		Column:  pos.Col,
		Kind:    kind,
	}
}

func (e *Error) sourceFor(source []byte) []byte {
	if source != nil && e.source != nil {
		return e.source
	}
	return source
}

func (e *Error) finding(source []byte) render.Finding {
	return render.Finding{
		Diagnostic: Diagnostic{Rule: e.Kind, Message: e.Message, Path: e.Filepath, Start: DiagnosticPosition{Line: e.Line, Column: e.Column}},
		Source:     e.sourceFor(source),
		EndColumn:  e.endColumn,
		RawScript:  e.rawScriptSource,
	}
}

// GetTemplateFields fields for formatting this error with Go template.
func (e *Error) GetTemplateFields(source []byte) *ErrorTemplateFields {
	return e.finding(source).TemplateFields()
}

// PrettyPrint prints the error with user-friendly way. It prints file name, source position, error
// message with colorful output and source snippet with indicator. When nil is set to source, no
// source snippet is not printed. To disable colorful output, set true to fatih/color.NoColor.
func (e *Error) PrettyPrint(w io.Writer, source []byte) {
	e.finding(source).PrettyPrint(w)
}

func compareErrors(lhs, rhs *Error) int {
	if c := strings.Compare(lhs.Filepath, rhs.Filepath); c != 0 {
		return c
	}
	if lhs.Line != rhs.Line {
		return lhs.Line - rhs.Line
	}
	if lhs.Column != rhs.Column {
		return lhs.Column - rhs.Column
	}
	if c := strings.Compare(lhs.Message, rhs.Message); c != 0 {
		return c
	}
	if lhs.endColumn != rhs.endColumn {
		return lhs.endColumn - rhs.endColumn
	}
	return strings.Compare(lhs.severity, rhs.severity)
}

func equalsErrors(lhs, rhs *Error) bool {
	return lhs.Filepath == rhs.Filepath &&
		lhs.Line == rhs.Line &&
		lhs.Column == rhs.Column &&
		lhs.endColumn == rhs.endColumn &&
		lhs.Message == rhs.Message &&
		lhs.severity == rhs.severity
}

// ErrorTemplateFields holds all fields to format one error message.
type ErrorTemplateFields = render.TemplateFields

// ErrorFormatter formats errors with the template given to the -format option.
type ErrorFormatter struct {
	*render.Formatter
}

// NewErrorFormatter creates new ErrorFormatter instance. Given format must contain at least one
// {{ }} placeholder. Escaped characters like \n in the format string are unescaped.
func NewErrorFormatter(format string) (*ErrorFormatter, error) {
	formatter, err := render.NewFormatter(format, Version, templateRules())
	if err != nil {
		return nil, err
	}
	return &ErrorFormatter{formatter}, nil
}

// PrintErrors prints the errors after formatting them with template.
func (f *ErrorFormatter) PrintErrors(out io.Writer, errs []*Error, src []byte) error {
	t := make([]*ErrorTemplateFields, 0, len(errs))
	for _, err := range errs {
		t = append(t, err.GetTemplateFields(src))
	}
	return f.Print(out, t)
}

// RegisterRule registers the rule. Registered rules are used to get description and index of error
// kinds when you use `kindDescription` or `kindIndex` functions in an error format template. This
// method can be called multiple times safely in parallel.
func (f *ErrorFormatter) RegisterRule(r Rule) {
	info := render.Rule{Name: r.Name()}
	if described, ok := r.(describedRule); ok {
		info.Description = described.Description()
	}
	f.Formatter.RegisterRule(info)
}

func templateRules() []render.Rule {
	var rules []render.Rule
	for _, descriptor := range builtinRuleDescriptors() {
		if descriptor.build == nil && descriptor.buildScripts == nil {
			rules = append(rules, descriptor.Clone())
		}
	}
	return rules
}
