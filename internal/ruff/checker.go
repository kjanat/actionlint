package ruff

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Run schedules a command through the host's bounded, cancellable process pool.
type Run func(args []string, stdin string, callback func([]byte, error) error)

// ExpressionEnd returns the byte length of a valid expression, including its
// closing delimiter. The input starts immediately after the opening ${{.
type ExpressionEnd func(string) (int, bool)

// Checker owns Python selection, invocation and diagnostic delivery. It knows
// nothing about the host AST: its callbacks bridge expressions and source maps.
type Checker struct {
	run                     Run
	wait                    func() error
	expressionEnd           ExpressionEnd
	flags                   []string
	workflowShell, jobShell *string
	mu                      sync.Mutex
}

// New creates a checker. The host must accept exit statuses 0 and 1, but not 2.
func New(run Run, wait func() error, expressionEnd ExpressionEnd, flags ...string) *Checker {
	return &Checker{run: run, wait: wait, expressionEnd: expressionEnd, flags: append([]string(nil), flags...)}
}

// Fork shares command scheduling but not workflow defaults or diagnostic state.
func (c *Checker) Fork() *Checker { return New(c.run, c.wait, c.expressionEnd, c.flags...) }

// UnsetEnvironment identifies ambient settings that bypass the stdout protocol.
// They must be removed after applying explicit child environment overrides too.
func UnsetEnvironment() []string { return []string{"RUFF_OUTPUT_FILE"} }

// WorkflowShell sets the workflow-level default shell.
func (c *Checker) WorkflowShell(shell *string) { c.workflowShell = shell }

// JobShell sets (or clears) the job-level default shell.
func (c *Checker) JobShell(shell *string) { c.jobShell = shell }

// Wait drains scheduled checks before diagnostics are read.
func (c *Checker) Wait() error { return c.wait() }

// Check schedules a Python script and serializes delivery to report.
func (c *Checker) Check(script string, shell *string, location string, config Config, report func(Diagnostic)) error {
	if config.Enabled != nil && !*config.Enabled {
		return nil
	}
	if shell == nil {
		shell = c.jobShell
	}
	if shell == nil {
		shell = c.workflowShell
	}
	if shell == nil || strings.Contains(*shell, "${{") {
		return nil
	}
	words := strings.Fields(*shell)
	if len(words) == 0 || (words[0] != "python" && words[0] != "python3") {
		return nil
	}
	source, valid, err := Sanitize(script, c.expressionEnd)
	if err != nil {
		return fmt.Errorf("ruff could not check Python script at %s: %w", location, err)
	}
	if !valid {
		return nil
	}
	defaults := arguments(config)
	args := make([]string, 1, len(defaults)+len(c.flags))
	args[0] = "check"
	args = append(args, c.flags...)
	args = append(args, defaults[1:]...)
	c.run(args, source, func(stdout []byte, err error) error {
		if err != nil {
			return fmt.Errorf("ruff failed while checking Python script at %s: %w", location, err)
		}
		diagnostics, err := Decode(stdout)
		if err != nil {
			return fmt.Errorf("ruff output for script at %s: %w", location, err)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, d := range diagnostics {
			report(d)
		}
		return nil
	})
	return nil
}

func arguments(config Config) []string {
	target := config.TargetVersion
	if target == "" {
		target = "py314"
	}
	selectRules, ignoreRules := config.Select, config.Ignore
	if selectRules != nil && len(selectRules) == 0 {
		// An empty --select is invalid. Equal-specificity ALL selectors disable
		// lint codes without suppressing Python syntax errors.
		selectRules, ignoreRules = []string{"ALL"}, []string{"ALL"}
	}
	if selectRules == nil {
		selectRules = []string{"F"}
	}
	args := []string{"check", "--isolated", "--target-version", target, "--select", strings.Join(selectRules, ","), "--ignore-noqa", "--no-fix", "--no-cache", "--output-format", "json", "--stdin-filename", "actionlint.py"}
	if len(ignoreRules) > 0 {
		args = append(args, "--ignore", strings.Join(ignoreRules, ","))
	}
	return append(args, "-")
}

// Sanitize masks templates with a neutral Python value, preserving Unicode
// columns and line breaks without introducing undefined Python identifiers.
// Scripts with malformed templates or interpolated token fragments are skipped.
func Sanitize(src string, expressionEnd ExpressionEnd) (string, bool, error) {
	var out strings.Builder
	var state pythonLexicalState
	for {
		start := strings.Index(src, "${{")
		if start < 0 {
			out.WriteString(src)
			return out.String(), true, nil
		}
		length, valid := expressionEnd(src[start+3:])
		if !valid || length < 2 || length > len(src)-start-3 {
			return "", false, nil
		}
		end := start + 3 + length
		out.WriteString(src[:start])
		state.consume(src[:start])
		if state.quote == 0 && !state.comment && templateTouchesPythonToken(src, start, end) {
			return "", false, nil
		}
		runes := []rune(src[start:end])
		for i, r := range runes {
			if r != '\n' && r != '\r' {
				runes[i] = ' '
			}
		}
		if state.comment {
			// Each physical line remains comment text, including the suffix
			// after the template. Empty lines can remain empty.
			first := true
			for i, r := range runes {
				if r == '\n' {
					first = true
				} else if r != '\r' && first {
					runes[i], first = '#', false
				}
			}
		} else {
			// Ordinary quoted strings require explicit continuations. Outside
			// strings, parentheses allow multiline expressions and empty lines.
			if state.quote != 0 && !state.triple {
				for i, r := range runes {
					if r != '\n' {
						continue
					}
					previous := i - 1
					if previous >= 0 && runes[previous] == '\r' {
						previous--
					}
					if previous < 0 || runes[previous] == '\n' {
						return "", true, errors.New("cannot preserve source positions for an empty expression line inside an ordinary quoted Python string")
					}
					runes[previous] = '\\'
				}
			}
			runes[0], runes[len(runes)-2], runes[len(runes)-1] = '(', '0', ')'
		}
		state.escaped = false
		out.WriteString(string(runes))
		src = src[end:]
	}
}

func templateTouchesPythonToken(source string, start, end int) bool {
	left, _ := utf8.DecodeLastRuneInString(source[:start])
	right, _ := utf8.DecodeRuneInString(source[end:])
	isTokenPart := func(r rune) bool {
		return r == '_' || r == '.' || r == '\'' || r == '"' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
	}
	return isTokenPart(left) || isTokenPart(right) || strings.HasPrefix(source[end:], "${{")
}

// pythonLexicalState distinguishes comment and string template placement while
// leaving Python syntax validation to Ruff.
type pythonLexicalState struct {
	quote   byte
	triple  bool
	comment bool
	escaped bool
}

func (s *pythonLexicalState) consume(text string) {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if s.comment {
			if c == '\n' {
				s.comment = false
			}
			continue
		}
		if s.escaped {
			s.escaped = false
			continue
		}
		if c == '\\' {
			s.escaped = true
			continue
		}
		if s.quote != 0 {
			if c == s.quote {
				if !s.triple {
					s.quote = 0
				} else if i+2 < len(text) && text[i+1] == c && text[i+2] == c {
					s.quote, s.triple = 0, false
					i += 2
				}
			}
			continue
		}
		switch c {
		case '#':
			s.comment = true
		case '\'', '"':
			s.quote = c
			s.triple = i+2 < len(text) && text[i+1] == c && text[i+2] == c
			if s.triple {
				i += 2
			}
		}
	}
}
