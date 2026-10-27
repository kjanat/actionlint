// Package backend adapts the shared snippet protocol to registered script tools.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	config "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/rule"
)

// Register uses advertised shell selectors, never an engine-name switch. Backends
// with language-specific preparation can instead supply their own registration.
func Register(descriptor rule.Descriptor, shells []string, backend analyzer.ScriptBackend) (tools.Registration, error) {
	return RegisterEngine(analyzer.ScriptEngineDescriptor{Rule: descriptor, Shells: shells}, backend)
}

// RegisterEngine routes both shell snippets and declared action-input sites.
func RegisterEngine(descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend) (tools.Registration, error) {
	return registerEngine(descriptor, backend, nil, nil)
}

func registerEngine(descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend, enabled func(tools.Context) bool, prepareInput func(tools.Script) (tools.PreparedInput, error)) (tools.Registration, error) {
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
	return tools.Registration{Descriptor: descriptor.Rule, ScriptInputs: append([]analyzer.ScriptInput(nil), descriptor.ScriptInputs...), RequiresExecution: len(descriptor.Shells) != 0, Build: func(c tools.Context) (tools.Runner, error) {
		return runner{descriptor: descriptor.Rule, templateContract: descriptor.TemplateContract, shells: selectors, inputs: inputs, backend: backend, enabled: enabled, context: c, prepareInput: prepareInput}, nil
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
	templateContract string
	descriptor       rule.Descriptor
	shells           map[string]struct{}
	inputs           map[analyzer.ScriptInput]struct{}
	backend          analyzer.ScriptBackend
	enabled          func(tools.Context) bool
	context          tools.Context
	prepareInput     func(tools.Script) (tools.PreparedInput, error)
}

func (r runner) scriptEnabled(cfg *config.Config) bool {
	if r.enabled == nil {
		return true
	}
	c := r.context
	c.Config, c.ProjectConfig, c.ProjectRoot = cfg, nil, ""
	if cfg != nil {
		resolved := *cfg
		// The prepared script already has its file overrides applied.
		resolved.Overrides = nil
		c.Config = &resolved
	}
	return r.enabled(c)
}

func (r runner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	var out []tools.Finding
	var failures error
	for i, script := range batch.Scripts {
		if err := ctx.Err(); err != nil {
			return out, errors.Join(failures, err)
		}
		if !r.scriptEnabled(script.Config) || config.DiagnosticLevel(script.Config, r.descriptor.Name) == "off" {
			continue
		}
		if script.Input != nil {
			if _, supported := r.inputs[*script.Input]; !supported {
				continue
			}
		} else {
			shell := scriptShell(script)
			if _, supported := r.shells[shell]; !supported {
				continue
			}
		}
		var options json.RawMessage
		var prepared tools.PreparedInput
		if r.prepareInput != nil && script.Input != nil {
			var err error
			prepared, err = r.prepareInput(script)
			if err != nil {
				failures = errors.Join(failures, err)
				continue
			}
			options = prepared.Config
		} else {
			prepared.Source = maskExpressions(script.Text)
		}
		if (r.prepareInput == nil || script.Input == nil) && script.Config != nil {
			settings := script.Config.Lint.Rules.Plugins.Rules[r.descriptor.Name].Options
			if settings != nil {
				data, err := json.Marshal(settings)
				if err != nil {
					return out, err
				}
				options = data
			}
		}
		if prepared.Skip {
			continue
		}
		if prepared.TemplateContract != "" && prepared.TemplateContract != r.templateContract {
			failures = errors.Join(failures, fmt.Errorf("tool %s requires template contract %q; backend declares %q", r.descriptor.Name, prepared.TemplateContract, r.templateContract))
			continue
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
		if validator, ok := r.backend.(analyzer.ScriptTemplateValidator); ok && prepared.TemplateContract != "" {
			available, err := validator.ValidateTemplates(ctx)
			if err != nil || !available {
				failures = errors.Join(failures, err)
				continue
			}
		}
		request := analyzer.ScriptRequest{Source: prepared.Source, Filename: "actionlint-script", Config: options}
		if script.Input != nil {
			request.Filename = inputFilename(*script.Input)
		}
		diagnostics, err := r.backend.Check(ctx, request)
		failures = errors.Join(failures, err)
		for _, d := range diagnostics {
			if r.templateContract == analyzer.TemplateEffectsContract && !d.TemplateEffect.Valid() ||
				r.templateContract != analyzer.TemplateEffectsContract && d.TemplateEffect != "" {
				failures = errors.Join(failures, fmt.Errorf("tool %s returned an undeclared template effect %q", r.descriptor.Name, d.TemplateEffect))
				continue
			}
			if d.Filename != "" && d.Filename != request.Filename {
				failures = errors.Join(failures, fmt.Errorf("tool %s returned an unrelated source %q", r.descriptor.Name, d.Filename))
				continue
			}
			if prepared.AcceptDiagnostic != nil && !prepared.AcceptDiagnostic(d) {
				continue
			}
			if prepared.OriginalLine != nil {
				hasEnd := d.EndLocation != (analyzer.ScriptPosition{})
				d.Location.Row = prepared.OriginalLine(d.Location.Row)
				if hasEnd {
					d.EndLocation.Row = prepared.OriginalLine(d.EndLocation.Row)
				}
				if d.Location.Row == 0 || hasEnd && d.EndLocation.Row == 0 {
					failures = errors.Join(failures, fmt.Errorf("tool %s configuration diagnostic: %s", r.descriptor.Name, d.Message))
					continue
				}
			}
			start, ok := script.Source.Position(d.Location.Row, d.Location.Column)
			end, endOK := script.Source.EndPos(d.EndLocation.Row, d.EndLocation.Column)
			if ok && endOK && (end.Line < start.Line || end.Line == start.Line && end.Col < start.Col) {
				failures = errors.Join(failures, fmt.Errorf("tool %s returned a reversed range", r.descriptor.Name))
				continue
			}
			if !ok || !endOK {
				start, endOK = *script.Key, false
			}
			finding := tools.Finding{Rule: r.descriptor.Name, Path: script.Path, Message: d.Message, Code: d.Code, Severity: d.Severity, DocumentationURL: d.URL, Start: diagnostic.Position{Line: start.Line, Column: start.Col}, Script: i, Content: script.Content}
			if endOK {
				finding.End = diagnostic.Position{Line: end.Line, Column: end.Col}
			}
			out = append(out, finding)
		}
	}
	return out, failures
}

func inputFilename(site analyzer.ScriptInput) string {
	extension := site.Language
	switch extension {
	case "python":
		extension = "py"
	case "javascript":
		extension = "js"
	case "typescript":
		extension = "ts"
	}
	return "actionlint-input/" + site.Language + "/" + site.Action + "/" + site.Input + "." + extension
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

func scriptShell(script tools.Script) string {
	shell := script.Execution.Shell
	if shell == "" && script.Shell != nil {
		shell = *script.Shell
	}
	command := shellCommand(shell)
	return strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/"))), ".exe")
}

func shellCommand(shell string) string {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		return ""
	}
	if quote := shell[0]; quote == '"' || quote == '\'' {
		end := strings.IndexByte(shell[1:], quote)
		if end < 0 {
			return ""
		}
		end++
		if end+1 < len(shell) {
			next, _ := utf8.DecodeRuneInString(shell[end+1:])
			if !unicode.IsSpace(next) {
				return ""
			}
		}
		return shell[1:end]
	}
	end := strings.IndexFunc(shell, unicode.IsSpace)
	if end < 0 {
		return shell
	}
	return shell[:end]
}
