package engine

import (
	"io"

	"actionlint.kjanat.dev/internal/render"
)

func (e *Error) finding(source []byte) render.Finding {
	return render.Finding{
		Diagnostic: Diagnostic{Rule: e.Rule, Message: e.Message, Path: e.Path, Start: DiagnosticPosition{Line: e.Start.Line, Column: e.Start.Column}},
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
