package actionlint

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

var (
	bold   = color.New(color.Bold)
	green  = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	gray   = color.New(color.FgHiBlack)
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
	endColumn int
	// source is the content of the file at Filepath when the error points at a file other than the
	// linted workflow. It replaces the workflow source for rendering the snippet.
	source []byte
}

// Error returns summary of the error as string.
func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s [%s]", e.Filepath, e.Line, e.Column, e.Message, e.Kind)
}

func (e *Error) String() string {
	return e.Error()
}

func errorAt(pos *Pos, kind string, msg string) *Error {
	return &Error{
		Message: msg,
		Line:    pos.Line,
		Column:  pos.Col,
		Kind:    kind,
	}
}

func errorfAt(pos *Pos, kind string, format string, args ...any) *Error {
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

// GetTemplateFields fields for formatting this error with Go template.
func (e *Error) GetTemplateFields(source []byte) *ErrorTemplateFields {
	source = e.sourceFor(source)
	snippet := ""
	end := e.Column
	if len(source) > 0 && e.Line > 0 {
		if l, ok := e.getLine(source); ok {
			snippet = l
			if i := e.getIndicator(l); i != "" {
				snippet += "\n" + i
				end = e.getEndColumn(l)
			}
		}
	}

	return &ErrorTemplateFields{
		Message:   e.Message,
		Filepath:  e.Filepath,
		Line:      e.Line,
		Column:    e.Column,
		Kind:      e.Kind,
		Snippet:   snippet,
		EndColumn: end,
	}
}

// PrettyPrint prints the error with user-friendly way. It prints file name, source position, error
// message with colorful output and source snippet with indicator. When nil is set to source, no
// source snippet is not printed. To disable colorful output, set true to fatih/color.NoColor.
func (e *Error) PrettyPrint(w io.Writer, source []byte) {
	source = e.sourceFor(source)
	_, _ = yellow.Fprint(w, e.Filepath)
	_, _ = gray.Fprint(w, ":")
	_, _ = fmt.Fprint(w, e.Line)
	_, _ = gray.Fprint(w, ":")
	_, _ = fmt.Fprint(w, e.Column)
	_, _ = gray.Fprint(w, ": ")
	_, _ = bold.Fprint(w, e.Message)
	_, _ = gray.Fprintf(w, " [%s]\n", e.Kind)

	if len(source) == 0 || e.Line <= 0 {
		return
	}
	line, ok := e.getLine(source)
	if !ok || len(line) < e.Column-1 {
		return
	}

	lnum := fmt.Sprintf("%d | ", e.Line)
	indent := strings.Repeat(" ", len(lnum)-2)
	_, _ = gray.Fprintf(w, "%s|\n", indent)
	_, _ = gray.Fprint(w, lnum)
	_, _ = fmt.Fprintln(w, line)
	_, _ = gray.Fprintf(w, "%s| ", indent)
	_, _ = green.Fprintln(w, e.getIndicator(line))
}

func (e *Error) getLine(source []byte) (string, bool) {
	s := bufio.NewScanner(bytes.NewReader(source))
	l := 0
	for s.Scan() {
		l++
		if l == e.Line {
			return s.Text(), true
		}
	}
	return "", false
}

func (e *Error) getEndColumn(line string) int {
	if e.endColumn >= e.Column {
		return e.endColumn
	}

	start, ok := byteOffsetAtColumn(line, e.Column)
	if !ok {
		return e.Column
	}

	col := e.Column
	for _, c := range line[start:] {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			break
		}
		col++
	}
	if col > e.Column {
		col--
	}
	return col
}

func (e *Error) getIndicator(line string) string {
	if e.Column <= 0 {
		return ""
	}

	start, ok := byteOffsetAtColumn(line, e.Column)
	if !ok {
		return ""
	}

	// Count width of non-space characters after '^' for underline
	uw := 0
	if end := e.getEndColumn(line); end >= e.Column {
		off, ok := byteOffsetAtColumn(line, end+1)
		if !ok {
			off = len(line)
		}
		uw = runewidth.StringWidth(line[start:off])
	}
	if uw > 0 {
		uw-- // Decrement for place for '^'
	}

	// Count width of spaces before '^'
	sw := runewidth.StringWidth(line[:start])
	return fmt.Sprintf("%s^%s", strings.Repeat(" ", sw), strings.Repeat("~", uw))
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
	return strings.Compare(lhs.Message, rhs.Message)
}

func equalsErrors(lhs, rhs *Error) bool {
	return lhs.Filepath == rhs.Filepath &&
		lhs.Line == rhs.Line &&
		lhs.Column == rhs.Column &&
		lhs.endColumn == rhs.endColumn &&
		lhs.Message == rhs.Message
}

// ErrorTemplateFields holds all fields to format one error message.
type ErrorTemplateFields struct {
	// Message is error message body.
	Message string `json:"message"`
	// Filepath is a canonical relative file path. This is empty when input was read from stdin.
	// When encoding into JSON, this field may be omitted when the file path is empty.
	Filepath string `json:"filepath,omitempty"`
	// Line is a line number of error position.
	Line int `json:"line"`
	// Column is a column number of error position.
	Column int `json:"column"`
	// Kind is a rule name the error belongs to.
	Kind string `json:"kind"`
	// Snippet is a code snippet and indicator to indicate where the error occurred.
	// When encoding into JSON, this field may be omitted when the snippet is empty.
	Snippet string `json:"snippet,omitempty"`
	// EndColumn is a column number where the error indicator (^~~~~~~) ends. When no indicator
	// can be shown, EndColumn is equal to Column.
	EndColumn int `json:"end_column"`
}

func unescapeBackslash(s string) string {
	// https://golang.org/ref/spec#Rune_literals
	r := strings.NewReplacer(
		`\a`, "\a",
		`\b`, "\b",
		`\f`, "\f",
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
		`\v`, "\v",
		`\\`, "\\",
	)
	return r.Replace(s)
}

func toPascalCase(s string) string {
	ss := strings.FieldsFunc(s, func(r rune) bool {
		alnum := 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9'
		return !alnum
	})
	for i, s := range ss {
		var c rune
		for _, c = range s {
			break
		}
		if 'a' <= c && c <= 'z' {
			ss[i] = strings.ToUpper(s[:1]) + s[1:]
		}
	}
	return strings.Join(ss, "")
}

type ruleTemplateFields struct {
	Name        string
	Description string
}

func compareRuleTemplateByName(lhs, rhs *ruleTemplateFields) int {
	return strings.Compare(lhs.Name, rhs.Name)
}

// ErrorFormatter is a formatter to format a slice of ErrorTemplateFields. It is used for
// formatting error messages with -format option.
type ErrorFormatter struct {
	temp    *template.Template
	rules   map[string]*ruleTemplateFields
	rulesMu sync.Mutex
}

// NewErrorFormatter creates new ErrorFormatter instance. Given format must contain at least one
// {{ }} placeholder. Escaped characters like \n in the format string are unescaped.
func NewErrorFormatter(format string) (*ErrorFormatter, error) {
	if !strings.Contains(format, "{{") {
		return nil, fmt.Errorf("template to format error messages must contain at least one {{ }} placeholder: %s", format)
	}

	r := map[string]*ruleTemplateFields{
		"syntax-check": {"syntax-check", "Checks for GitHub Actions workflow syntax"},
	}

	funcs := template.FuncMap(map[string]any{
		"json": func(data any) (string, error) {
			var b strings.Builder
			enc := json.NewEncoder(&b)
			if err := enc.Encode(data); err != nil {
				return "", fmt.Errorf("could not encode template value into JSON: %w", err)
			}
			return b.String(), nil
		},
		"replace": func(s string, oldnew ...string) string {
			return strings.NewReplacer(oldnew...).Replace(s)
		},
		"toPascalCase": toPascalCase,
		"getVersion":   getCommandVersion,
		"allKinds": func() []*ruleTemplateFields {
			ret := make([]*ruleTemplateFields, 0, len(r))
			for _, e := range r {
				ret = append(ret, e)
			}
			slices.SortFunc(ret, compareRuleTemplateByName)
			return ret
		},
	})
	t, err := template.New("error formatter").Funcs(funcs).Parse(unescapeBackslash(format))
	if err != nil {
		return nil, fmt.Errorf("template %q to format error messages could not be parsed: %w", format, err)
	}

	return &ErrorFormatter{t, r, sync.Mutex{}}, nil
}

// Print formats the slice of template fields and prints it with given writer.
func (f *ErrorFormatter) Print(out io.Writer, t []*ErrorTemplateFields) error {
	if err := f.temp.Execute(out, t); err != nil {
		return fmt.Errorf("could not format error messages: %w", err)
	}
	return nil
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
	// Synchronize access to f.rules (#370)
	f.rulesMu.Lock()
	defer f.rulesMu.Unlock()

	n := r.Name()
	if _, ok := f.rules[n]; !ok {
		f.rules[n] = &ruleTemplateFields{n, r.Description()}
	}
}
