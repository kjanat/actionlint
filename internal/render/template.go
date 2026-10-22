package render

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"text/template"

	"actionlint.kjanat.dev/rule"
)

// TemplateFields holds all fields to format one finding with a Go template.
type TemplateFields struct {
	// Message is error message body.
	Message string `json:"message"`
	// Filepath is a canonical relative file path. This is empty when input was read from stdin.
	Filepath string `json:"filepath,omitempty"`
	// Line is a line number of error position.
	Line int `json:"line"`
	// Column is a column number of error position.
	Column int `json:"column"`
	// Kind is a rule name the error belongs to.
	Kind string `json:"kind"`
	// Snippet is a code snippet and indicator to indicate where the error occurred.
	Snippet string `json:"snippet,omitempty"`
	// EndColumn is a column number where the error indicator (^~~~~~~) ends. When no indicator
	// can be shown, EndColumn is equal to Column.
	EndColumn int `json:"end_column"`
}

// Rule is the metadata a template may list through allKinds.
type Rule = rule.Descriptor

func compareRules(lhs, rhs *Rule) int {
	return strings.Compare(lhs.Name, rhs.Name)
}

type ruleSet struct {
	mu    sync.Mutex
	rules map[string]*Rule
}

func newRuleSet(rules []Rule) *ruleSet {
	set := &ruleSet{rules: map[string]*Rule{}}
	for _, rule := range rules {
		set.register(rule)
	}
	return set
}

func (s *ruleSet) register(rule Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rules[rule.Name]; !exists {
		s.rules[rule.Name] = new(rule.Clone())
	}
}

func (s *ruleSet) sorted() []*Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	ret := make([]*Rule, 0, len(s.rules))
	for _, rule := range s.rules {
		ret = append(ret, rule)
	}
	slices.SortFunc(ret, compareRules)
	return ret
}

// Formatter renders TemplateFields with a user-supplied Go template.
type Formatter struct {
	temp  *template.Template
	rules *ruleSet
}

// NewFormatter parses a template containing at least one {{ }} placeholder.
// Escaped characters such as \n are unescaped. getVersion and allKinds read the
// supplied version and initial rules; RegisterRule adds rules later.
func NewFormatter(format string, version func() string, rules []Rule) (*Formatter, error) {
	return newFormatter(format, version, newRuleSet(rules))
}

func newFormatter(format string, version func() string, known *ruleSet) (*Formatter, error) {
	if !strings.Contains(format, "{{") {
		return nil, fmt.Errorf("template to format error messages must contain at least one {{ }} placeholder: %s", format)
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
		"getVersion":   version,
		"allKinds":     known.sorted,
	})
	t, err := template.New("error formatter").Funcs(funcs).Parse(unescapeBackslash(format))
	if err != nil {
		return nil, fmt.Errorf("template %q to format error messages could not be parsed: %w", format, err)
	}
	return &Formatter{t, known}, nil
}

// Print formats the slice of template fields and prints it with given writer.
func (f *Formatter) Print(out io.Writer, t []*TemplateFields) error {
	if err := f.temp.Execute(out, t); err != nil {
		return fmt.Errorf("could not format error messages: %w", err)
	}
	return nil
}

// RegisterRule makes a rule visible to allKinds. It is safe to call concurrently.
func (f *Formatter) RegisterRule(rule Rule) {
	f.rules.register(rule)
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
