package ruff

import (
	"fmt"
	"strings"
	"sync"
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
	workflowShell, jobShell *string
	mu                      sync.Mutex
}

// New creates a checker. The host must accept exit statuses 0 and 1, but not 2.
func New(run Run, wait func() error, expressionEnd ExpressionEnd) *Checker {
	return &Checker{run: run, wait: wait, expressionEnd: expressionEnd}
}

// Fork shares command scheduling but not workflow defaults or diagnostic state.
func (c *Checker) Fork() *Checker { return New(c.run, c.wait, c.expressionEnd) }

// WorkflowShell sets the workflow-level default shell.
func (c *Checker) WorkflowShell(shell *string) { c.workflowShell = shell }

// JobShell sets (or clears) the job-level default shell.
func (c *Checker) JobShell(shell *string) { c.jobShell = shell }

// Wait drains scheduled checks before diagnostics are read.
func (c *Checker) Wait() error { return c.wait() }

// Check schedules a Python script and serializes delivery to report.
func (c *Checker) Check(script string, shell *string, location string, config Config, report func(Diagnostic)) {
	if config.Enabled != nil && !*config.Enabled {
		return
	}
	if shell == nil {
		shell = c.jobShell
	}
	if shell == nil {
		shell = c.workflowShell
	}
	if shell == nil || strings.Contains(*shell, "${{") {
		return
	}
	words := strings.Fields(*shell)
	if len(words) == 0 || (words[0] != "python" && words[0] != "python3") {
		return
	}
	source, valid := Sanitize(script, c.expressionEnd)
	if !valid {
		return
	} // Malformed templates belong to the expression checker.
	c.run(arguments(config), source, func(stdout []byte, err error) error {
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
func Sanitize(src string, expressionEnd ExpressionEnd) (string, bool) {
	var out strings.Builder
	for {
		start := strings.Index(src, "${{")
		if start < 0 {
			out.WriteString(src)
			return out.String(), true
		}
		length, valid := expressionEnd(src[start+3:])
		if !valid || length < 2 || length > len(src)-start-3 {
			return "", false
		}
		end := start + 3 + length
		out.WriteString(src[:start])
		runes := []rune(src[start:end])
		for i, r := range runes {
			if r != '\n' && r != '\r' {
				runes[i] = ' '
			}
		}
		runes[0], runes[len(runes)-2], runes[len(runes)-1] = '(', '0', ')'
		out.WriteString(string(runes))
		src = src[end:]
	}
}
