package ruff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	pythonprofile "actionlint.kjanat.dev/v2/internal/tools/profile/python"
)

// Let Ruff resolve its own cwd spelling, including Windows short paths.
const stdinFilename = "actionlint.py"

// Run schedules a command through the host's bounded, cancellable process pool.
type Run func(args []string, stdin string, callback func([]byte, error) error)

// Checker owns Python invocation and diagnostic delivery; the Python
// preparation profile selects and masks scripts. It knows nothing about the
// host AST: its callbacks bridge expressions and source maps.
type Checker struct {
	commandPolicy
	backend       analyzer.ScriptBackend
	ctx           context.Context
	pending       []analyzerhost.Task[Diagnostic]
	run           Run
	wait          func() error
	expressionEnd pythonprofile.ExpressionEnd
	beforeCheck   func() (bool, error)
	mu            sync.Mutex
}

// New creates a checker. The host must accept exit statuses 0 and 1, but not 2.
func New(run Run, wait func() error, expressionEnd pythonprofile.ExpressionEnd, flags ...string) *Checker {
	return &Checker{run: run, wait: wait, expressionEnd: expressionEnd, flags: append([]string(nil), flags...)}
}

// NewBackend binds a transport-independent engine to actionlint's Python
// selection, sanitization, and result filtering. No process is resolved.
func NewBackend(ctx context.Context, backend analyzer.ScriptBackend, expressionEnd pythonprofile.ExpressionEnd) *Checker {
	return &Checker{backend: backend, ctx: ctx, expressionEnd: expressionEnd}
}

// Fork shares command scheduling and copies checker configuration.
func (c *Checker) Fork() *Checker {
	child := New(c.run, c.wait, c.expressionEnd, c.flags...)
	if c.backend != nil {
		child = NewBackend(c.ctx, c.backend, c.expressionEnd)
	}
	child.workingDirectory = c.workingDirectory
	child.beforeCheck = c.beforeCheck
	child.commandError = c.commandError
	return child
}

// WorkingDirectory sets the child process directory used for its stdin filename.
func (c *Checker) WorkingDirectory(directory string) { c.workingDirectory = directory }

// OptionalVersion skips incompatible automatically discovered executables.
func (c *Checker) OptionalVersion(check *Compatibility, ctx context.Context, warning func(error)) {
	c.beforeCheck = func() (bool, error) { return check.available(ctx, c.run, c.wait, warning) }
}

// UnsetEnvironment identifies ambient settings that bypass the stdout protocol.
// They must be removed after applying explicit child environment overrides too.
func UnsetEnvironment() []string { return []string{"RUFF_OUTPUT_FILE"} }

// Wait drains scheduled checks before diagnostics are read.
func (c *Checker) Wait() error {
	if c.backend == nil {
		return c.wait()
	}
	tasks := c.pending
	c.pending = nil
	_, err := analyzerhost.RunTasks(c.ctx, tasks)
	return err
}

// Check schedules a Python script and serializes delivery to report.
func (c *Checker) Check(script string, shell *string, location string, config Config, report func(Diagnostic)) error {
	if config.Enabled != nil && !*config.Enabled {
		return nil
	}
	if shell == nil || strings.Contains(*shell, "${{") {
		return nil
	}
	if !pythonprofile.IsShell(*shell) {
		return nil
	}
	if c.backend == nil {
		if err := c.validateCommand(location); err != nil {
			return err
		}
	}
	source, placeholders, valid, err := pythonprofile.Mask(script, c.expressionEnd)
	if err != nil {
		return fmt.Errorf("ruff could not check Python script at %s: %w", location, err)
	}
	if !valid {
		return nil
	}
	templates := strings.Contains(script, "${{")
	if c.backend != nil && templates && backendTemplateContract(c.backend) != analyzer.TemplateEffectsContract {
		return fmt.Errorf("ruff expression masking requires template contract %q", analyzer.TemplateEffectsContract)
	}
	if c.beforeCheck != nil {
		if available, err := c.beforeCheck(); err != nil || !available {
			return err
		}
	}
	directory := c.workingDirectory
	if directory == "" {
		directory = "."
	}
	var directoryInfo os.FileInfo
	if c.backend == nil {
		directoryInfo, err = os.Stat(directory)
		if err != nil {
			return fmt.Errorf("ruff stdin directory for script at %s: %w", location, err)
		}
		if !directoryInfo.IsDir() {
			return fmt.Errorf("ruff stdin directory for script at %s is not a directory: %q", location, directory)
		}
	}
	if config.TargetVersion == "" {
		config.TargetVersion = pythonprofile.ShellTarget(*shell)
	}
	deliver := func(diagnostics []Diagnostic, err error) error {
		if err != nil && len(diagnostics) == 0 {
			return fmt.Errorf("ruff failed while checking Python script at %s: %w", location, err)
		}

		for i := range diagnostics {
			diagnostic := &diagnostics[i]
			if diagnostic.Code == "" {
				diagnostic.Code = diagnostic.Name
			}
			if c.backend != nil {
				contract := backendTemplateContract(c.backend)
				if contract == analyzer.TemplateEffectsContract && !diagnostic.TemplateEffect.Valid() ||
					contract != analyzer.TemplateEffectsContract && diagnostic.TemplateEffect != "" {
					return fmt.Errorf("ruff backend returned undeclared template effect %q", diagnostic.TemplateEffect)
				}
				if diagnostic.Filename != stdinFilename || diagnostic.Code == "" || diagnostic.Message == "" || diagnostic.Location.Row < 1 || diagnostic.Location.Column < 1 {
					return fmt.Errorf("invalid backend diagnostic for %s: %+v", stdinFilename, diagnostic)
				}
				continue
			}
			parent, err := os.Stat(filepath.Dir(diagnostic.Filename))
			if !filepath.IsAbs(diagnostic.Filename) || filepath.Base(diagnostic.Filename) != stdinFilename || err != nil || !os.SameFile(directoryInfo, parent) {
				return fmt.Errorf("ruff output for script at %s refers to unexpected file %q; expected %q in %q", location, diagnostic.Filename, stdinFilename, directory)
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, d := range diagnostics {
			if !placeholders.Accept(d) {
				continue
			}
			report(d)
		}
		return err
	}
	if c.backend != nil {
		options, err := engineOptions(config)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(options)
		if err != nil {
			return err
		}
		request := analyzer.ScriptRequest{Source: source, Filename: stdinFilename, Config: encoded}
		c.pending = append(c.pending, func(ctx context.Context) ([]Diagnostic, error) {
			if validator, ok := c.backend.(analyzer.ScriptConfigValidator); ok {
				if err := validator.ValidateConfig(ctx, request.Config); err != nil {
					return nil, err
				}
			}
			if validator, ok := c.backend.(analyzer.ScriptTemplateValidator); ok && templates {
				if available, err := validator.ValidateTemplates(ctx); err != nil || !available {
					return nil, err
				}
			}
			findings, err := c.backend.Check(ctx, request)
			return nil, deliver(findings, err)
		})
	} else {
		defaults := arguments(config)
		native, err := nativeArguments(config)
		if err != nil {
			return err
		}
		args := make([]string, 1, len(defaults)+len(c.flags))
		args[0] = "check"
		args = append(args, native...)
		args = append(args, c.flags...)
		args = append(args, defaults[1:]...)
		c.run(args, source, func(stdout []byte, err error) error {
			if err != nil {
				return deliver(nil, err)
			}
			findings, decodeErr := Decode(stdout)
			return deliver(findings, errors.Join(err, decodeErr))
		})
	}
	return nil
}

func arguments(config Config) []string {
	selectRules, ignoreRules := config.Select, config.Ignore
	if selectRules != nil && len(selectRules) == 0 {
		// An empty --select is invalid. Equal-specificity ALL selectors disable
		// lint codes without suppressing Python syntax errors.
		selectRules, ignoreRules = []string{"ALL"}, []string{"ALL"}
	}
	args := []string{"check", "--isolated", "--ignore-noqa", "--no-fix", "--no-cache", "--output-format", "json", "--stdin-filename", stdinFilename, "--extension", "py:python"}
	if config.TargetVersion != "" && config.Native["target-version"] == nil {
		args = append(args, "--target-version="+config.TargetVersion)
	}
	if selectRules != nil {
		args = append(args, "--select="+strings.Join(selectRules, ","))
	}
	if len(ignoreRules) > 0 {
		args = append(args, "--ignore="+strings.Join(ignoreRules, ","))
	}
	return append(args, "-")
}

// Ruff accepts either a file path or an inline TOML assignment for --config.
// Existing files take precedence, even when their names contain an equals sign.
// Ruff itself validates the assignment's key and value after this boundary check.
func ruffInlineConfig(value, directory string) bool {
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	if _, err := os.Stat(path); err == nil {
		return false
	}
	var quote rune
	escaped := false
	for _, char := range value {
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case char == '\\' && quote == '"':
				escaped = true
			case char == quote:
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '=':
			return true
		case '#', '\n', '\r':
			return false
		}
	}
	return false
}
