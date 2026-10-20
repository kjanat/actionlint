// Package backend adapts the shared snippet protocol to registered script tools.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/analyzerhost"
	config "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/rule"
)

// Register uses advertised shell selectors, never an engine-name switch. Backends
// with language-specific preparation can instead supply their own registration.
func Register(descriptor rule.Descriptor, shells []string, backend analyzer.ScriptBackend) (tools.Registration, error) {
	if descriptor.Name == "" || len(shells) == 0 || backend == nil {
		return tools.Registration{}, errors.New("script engine requires a rule, shell selectors and a backend")
	}
	shells = slices.Clone(shells)
	for _, shell := range shells {
		if shell == "" || strings.ContainsAny(shell, " /\\\t\r\n") {
			return tools.Registration{}, fmt.Errorf("invalid script shell selector %q", shell)
		}
	}
	return tools.Registration{Descriptor: descriptor, RequiresExecution: true, Build: func(tools.Context) (tools.Runner, error) { return runner{descriptor, shells, backend}, nil }}, nil
}

type runner struct {
	descriptor rule.Descriptor
	shells     []string
	backend    analyzer.ScriptBackend
}

func (r runner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	var out []tools.Finding
	var failures error
	for i, script := range batch.Scripts {
		if err := ctx.Err(); err != nil {
			return out, errors.Join(failures, err)
		}
		if config.DiagnosticLevel(script.Config, r.descriptor.Name) == "off" {
			continue
		}
		command, _, _ := strings.Cut(script.Execution.Shell, " ")
		shell := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/"))), ".exe")
		if !slices.Contains(r.shells, shell) {
			continue
		}
		var options json.RawMessage
		if script.Config != nil {
			settings := script.Config.Lint.Rules.Plugins.Rules[r.descriptor.Name].Options
			if settings != nil {
				data, err := json.Marshal(settings)
				if err != nil {
					return out, err
				}
				options = data
			}
		}
		if err := analyzerhost.ValidateOptions(r.descriptor.OptionsSchema, options); err != nil {
			failures = errors.Join(failures, fmt.Errorf("tool %s options: %w", r.descriptor.Name, err))
			continue
		}
		if validator, ok := r.backend.(analyzer.ScriptConfigValidator); ok {
			if err := validator.ValidateConfig(ctx, options); err != nil {
				failures = errors.Join(failures, err)
				continue
			}
		}
		request := analyzer.ScriptRequest{Source: maskExpressions(script.Text), Filename: "actionlint-script", Config: options}
		diagnostics, err := r.backend.Check(ctx, request)
		failures = errors.Join(failures, err)
		for _, d := range diagnostics {
			if d.Filename != "" && d.Filename != request.Filename {
				failures = errors.Join(failures, fmt.Errorf("tool %s returned an unrelated source %q", r.descriptor.Name, d.Filename))
				continue
			}
			start, ok := script.Source.Position(d.Location.Row, d.Location.Column)
			end, endOK := script.Source.EndPos(d.EndLocation.Row, d.EndLocation.Column)
			if !ok || !endOK {
				start, end = script.Key, nil
			}
			finding := tools.Finding{Rule: r.descriptor.Name, Path: script.Path, Message: d.Message, Code: d.Code, Severity: d.Severity, DocumentationURL: d.URL, Start: diagnostic.Position{Line: start.Line, Column: start.Col}, Script: i, Content: script.Content}
			if end != nil {
				finding.End = diagnostic.Position{Line: end.Line, Column: end.Col}
			}
			out = append(out, finding)
		}
	}
	return out, failures
}

func maskExpressions(text string) string {
	var out strings.Builder
	for {
		start := strings.Index(text, "${{")
		if start < 0 {
			out.WriteString(text)
			return out.String()
		}
		lexer := expression.NewLexer(text[start+3:])
		if _, err := expression.NewParser().Parse(lexer); err != nil {
			out.WriteString(text)
			return out.String()
		}
		end := start + 3 + lexer.Offset()
		out.WriteString(text[:start])
		for _, r := range text[start:end] {
			if r == '\n' || r == '\r' {
				out.WriteRune(r)
			} else {
				out.WriteByte('_')
			}
		}
		text = text[end:]
	}
}
