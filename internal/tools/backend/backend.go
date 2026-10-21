// Package backend adapts the shared snippet protocol to registered script tools.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
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
	return RegisterEngine(analyzer.ScriptEngineDescriptor{Rule: descriptor, Shells: shells}, backend)
}

// RegisterEngine routes both shell snippets and declared action-input sites.
func RegisterEngine(descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend) (tools.Registration, error) {
	descriptor = descriptor.Clone()
	if backend == nil {
		return tools.Registration{}, errors.New("script engine requires a rule, shell selectors and a backend")
	}
	if err := analyzer.ValidateScriptInputs(descriptor.ScriptInputs); err != nil {
		return tools.Registration{}, err
	}
	if descriptor.Rule.Name == "" || len(descriptor.Shells)+len(descriptor.ScriptInputs) == 0 {
		return tools.Registration{}, errors.New("script engine requires a rule and script selectors")
	}
	selectors, err := shellCatalogue(descriptor.Rule, descriptor.Shells)
	if err != nil {
		return tools.Registration{}, err
	}
	inputs := make(map[analyzer.ScriptInput]struct{}, len(descriptor.ScriptInputs))
	for _, input := range descriptor.ScriptInputs {
		inputs[input] = struct{}{}
	}
	return tools.Registration{Descriptor: descriptor.Rule, ScriptInputs: append([]analyzer.ScriptInput(nil), descriptor.ScriptInputs...), RequiresExecution: len(descriptor.Shells) != 0, Build: func(tools.Context) (tools.Runner, error) {
		return runner{descriptor: descriptor.Rule, shells: selectors, inputs: inputs, backend: backend}, nil
	}}, nil
}

func shellCatalogue(descriptor rule.Descriptor, shells []string) (map[string]struct{}, error) {
	if descriptor.Name == "" {
		return nil, errors.New("script engine requires a rule, shell selectors and a backend")
	}
	selectors := make(map[string]struct{}, len(shells))
	for _, shell := range shells {
		if shell == "" || strings.ContainsAny(shell, " /\\\t\r\n") {
			return nil, fmt.Errorf("invalid script shell selector %q", shell)
		}
		selectors[shell] = struct{}{}
	}
	return selectors, nil
}

type runner struct {
	descriptor rule.Descriptor
	shells     map[string]struct{}
	inputs     map[analyzer.ScriptInput]struct{}
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
		if script.Input != nil {
			if _, supported := r.inputs[*script.Input]; !supported {
				continue
			}
		} else {
			command, _, _ := strings.Cut(script.Execution.Shell, " ")
			shell := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/"))), ".exe")
			if _, supported := r.shells[shell]; !supported {
				continue
			}
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
			if ok && endOK && (end.Line < start.Line || end.Line == start.Line && end.Col < start.Col) {
				failures = errors.Join(failures, fmt.Errorf("tool %s returned a reversed range", r.descriptor.Name))
				continue
			}
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
