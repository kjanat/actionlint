package output

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"text/template"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

type TemplateFields struct {
	Message string `json:"message"`
	// Filepath is a canonical relative file path. This is empty when input was read from stdin.
	Filepath string `json:"filepath,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	// Kind is the name of the rule that reported the diagnostic.
	Kind string `json:"kind"`
	// Code is the native diagnostic identifier, when supplied by the check.
	Code string `json:"code,omitempty"`
	// Severity is info, warning or error; empty means the default error level.
	Severity string `json:"severity,omitempty"`
	// Snippet is a source excerpt and an indicator of the diagnostic position.
	Snippet string `json:"snippet,omitempty"`
	// EndColumn is a column number where the error indicator (^~~~~~~) ends. When no indicator
	// can be shown, EndColumn is equal to Column.
	EndColumn int `json:"end_column"`
}

func compareRules(lhs, rhs *rule.Descriptor) int {
	return strings.Compare(lhs.Name, rhs.Name)
}

type ruleSet struct {
	mu    sync.Mutex
	rules map[string]*rule.Descriptor
}

func newRuleSet(rules []rule.Descriptor) *ruleSet {
	set := &ruleSet{rules: map[string]*rule.Descriptor{}}
	for _, rule := range rules {
		set.register(rule)
	}
	return set
}

func (s *ruleSet) register(descriptor rule.Descriptor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.rules[descriptor.Name]; !exists {
		s.rules[descriptor.Name] = new(descriptor.Clone())
	}
}

func (s *ruleSet) sorted() []*rule.Descriptor {
	s.mu.Lock()
	defer s.mu.Unlock()
	ret := make([]*rule.Descriptor, 0, len(s.rules))
	for _, rule := range s.rules {
		ret = append(ret, rule)
	}
	slices.SortFunc(ret, compareRules)
	return ret
}

func (s *ruleSet) lookup(name string) *rule.Descriptor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rules[name]
}

type Formatter struct {
	temp  *template.Template
	rules *ruleSet
}

// NewFormatter parses a template containing at least one {{ }} placeholder.
// Escaped characters such as \n are unescaped.
func NewFormatter(format string, version func() string, rules []rule.Descriptor) (*Formatter, error) {
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
		"escapeData":     githubCommandData.Replace,
		"escapeProperty": githubCommandProperty.Replace,
		"toPascalCase":   toPascalCase,
		"getVersion":     version,
		"allKinds":       known.sorted,
	})
	t, err := template.New("error formatter").Funcs(funcs).Parse(unescapeBackslash(format))
	if err != nil {
		return nil, fmt.Errorf("template %q to format error messages could not be parsed: %w", format, err)
	}
	return &Formatter{t, known}, nil
}

func (f *Formatter) Print(out io.Writer, t []*TemplateFields) error {
	if err := f.temp.Execute(out, t); err != nil {
		return fmt.Errorf("could not format error messages: %w", err)
	}
	return nil
}

// RegisterRule is safe to call concurrently.
func (f *Formatter) RegisterRule(descriptor rule.Descriptor) {
	f.rules.register(descriptor)
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
