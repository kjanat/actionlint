// Package backend adapts the shared snippet protocol to registered script tools.
package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	config "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/profile"
	"actionlint.kjanat.dev/v2/internal/tools/profile/mask"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/provider/process"
	"actionlint.kjanat.dev/v2/rule"
)

// Register uses advertised shell selectors, never an engine-name switch. Backends
// with language-specific preparation can instead supply their own registration.
func Register(descriptor rule.Descriptor, shells []string, backend analyzer.ScriptBackend) (tools.Registration, error) {
	return RegisterEngine(analyzer.ScriptEngineDescriptor{Rule: descriptor, Shells: shells}, backend)
}

// RegisterEngine routes both shell snippets and declared action-input sites.
func RegisterEngine(descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend) (tools.Registration, error) {
	return registerEngine(descriptor, backend, engineOptions{})
}

// engineOptions configure a runner beyond its descriptor and backend.
type engineOptions struct {
	// enabled further restricts the scripts of a configuration.
	enabled func(tools.Context) bool
	// prepare replaces expression masking and plugin options for every
	// selected script.
	prepare func(tools.Script) (profile.Prepared, error)
	// batch replaces prepare with a preparation bound to one batch, which
	// may reject the settings of its scopes once. With it, run scripts
	// reach the preparation even when no shell selects them; the profile
	// skips the ones it does not prepare.
	batch func(tools.Batch) (func(tools.Script) (profile.Prepared, error), error)
	// filename is the virtual name of run snippets; its extension also names
	// action-input snippets. Empty keeps "actionlint-script".
	filename string
	findings findingCapabilities
}

// findingCapabilities declare what a backend's diagnostics may carry and
// how their messages read.
type findingCapabilities struct {
	// fixes admits suggested edits.
	fixes bool
	// reportedSources admits findings in files the engine read.
	reportedSources bool
	// messageTemplate formats messages from {code}, {severity}, {row},
	// {column} and {message}; empty keeps the engine's message.
	messageTemplate string
	// severities maps engine severities to host severities; an unmapped
	// severity becomes a warning. Empty passes severities through.
	severities map[string]provider.Severity
}

func registerEngine(descriptor analyzer.ScriptEngineDescriptor, backend analyzer.ScriptBackend, options engineOptions) (tools.Registration, error) {
	descriptor = descriptor.Clone()
	if backend == nil {
		return tools.Registration{}, errors.New("script engine requires a rule, shell selectors and a backend")
	}
	if err := analyzer.ValidateScriptInputs(descriptor.ScriptInputs); err != nil {
		return tools.Registration{}, err
	}
	if descriptor.Rule.Name == "" || len(descriptor.Shells)+len(descriptor.ScriptInputs) == 0 && options.batch == nil {
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
	// An engine's own capabilities admit what its diagnostics carry; its
	// transport already rejected anything it did not declare.
	options.findings.fixes = options.findings.fixes || descriptor.Fixes
	options.findings.reportedSources = options.findings.reportedSources || descriptor.ReportedSources
	directory := slices.Contains(descriptor.Resources, analyzer.ToolDirectory)
	return tools.Registration{Descriptor: descriptor.Rule, ScriptInputs: append([]analyzer.ScriptInput(nil), descriptor.ScriptInputs...), RequiresExecution: len(descriptor.Shells) != 0 || directory, Build: func(c tools.Context) (tools.Runner, error) {
		return runner{descriptor: descriptor.Rule, templateContract: descriptor.TemplateContract, shells: selectors, inputs: inputs, backend: backend, context: c, options: options, directory: directory}, nil
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
	context          tools.Context
	options          engineOptions
	// directory sends the working directory to an engine that requested the
	// directory resource.
	directory bool
}

// processRunner is a backend that runs one native process per check: it
// takes preparation variables, reports the files the engine read and may run
// checks at once, each holding a slot of the shared process pool.
type processRunner interface {
	Run(context.Context, analyzer.ScriptRequest, map[string]string) (process.Result, error)
	Concurrent() bool
}

func (r runner) scriptEnabled(cfg *config.Config) bool {
	if r.options.enabled == nil {
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
	return r.options.enabled(c)
}

// check is one prepared request for the batch script at index. Scope is
// the script's, which bounds a directory grant.
type check struct {
	index    int
	prepared profile.Prepared
	request  analyzer.ScriptRequest
	scope    *tools.Scope
}

// outcome is the backend's answer to one check. Copies, set when the check
// had a directory grant, holds the display copy of each reported source read
// through that grant; reported sources missing from it are unreadable.
type outcome struct {
	diagnostics []analyzer.ScriptDiagnostic
	sources     []string
	directory   string
	copies      map[string]reportedFile
	err         error
}

// Analyze prepares the selected scripts, checks them, concurrently when the
// backend allows, and maps the diagnostics back in script order.
func (r runner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	checks, failures := r.prepare(ctx, batch)
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(failures, err)
	}
	outcomes := r.check(ctx, checks)
	var out []tools.Finding
	for i, c := range checks {
		findings, err := r.findings(c, batch.Scripts[c.index], outcomes[i])
		out = append(out, findings...)
		failures = errors.Join(failures, outcomes[i].err, err)
	}
	return out, failures
}

// prepare selects, prepares and validates the scripts this tool checks.
func (r runner) prepare(ctx context.Context, batch tools.Batch) ([]check, error) {
	var checks []check
	var failures error
	_, reporting := r.backend.(processRunner)
	prepare := r.options.prepare
	if r.options.batch != nil {
		var err error
		prepare, err = r.options.batch(batch)
		failures = errors.Join(failures, err)
	}
	for i, script := range batch.Scripts {
		if err := ctx.Err(); err != nil {
			return checks, failures
		}
		if !r.scriptEnabled(script.Config) || config.DiagnosticLevel(script.Config, r.descriptor.Name) == "off" {
			continue
		}
		if script.Input != nil {
			if _, supported := r.inputs[*script.Input]; !supported {
				continue
			}
		} else if len(r.shells) != 0 || r.options.batch == nil {
			shell := scriptShell(script)
			if _, supported := r.shells[shell]; !supported {
				continue
			}
		}
		var options json.RawMessage
		var prepared profile.Prepared
		if prepare != nil {
			var err error
			prepared, err = prepare(script)
			if err != nil {
				failures = errors.Join(failures, err)
				continue
			}
			options = prepared.Config
		} else {
			prepared.Source = mask.Expressions(script.Text)
			if script.Config != nil {
				settings := script.Config.Lint.Rules.Plugins.Rules[r.descriptor.Name].Options
				if settings != nil {
					data, err := json.Marshal(settings)
					if err != nil {
						return checks, errors.Join(failures, err)
					}
					options = data
				}
			}
		}
		if prepared.Skip != "" {
			r.debug("%s: skipped a script in %s: %s", r.descriptor.Name, script.Path, prepared.Skip)
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
		request := analyzer.ScriptRequest{Source: prepared.Source, Filename: r.filename(script), Config: options, Excluded: prepared.Excluded}
		// Process backends and engines that requested the directory resource
		// receive the directory.
		if (reporting || r.directory) && script.Execution.DirectoryKnown {
			request.Directory = script.Execution.Directory
		}
		checks = append(checks, check{index: i, prepared: prepared, request: request, scope: script.Scope})
	}
	return checks, failures
}

// check runs every request, in parallel for a concurrent process backend.
// Each process still waits for a slot of the shared pool.
func (r runner) check(ctx context.Context, checks []check) []outcome {
	outcomes := make([]outcome, len(checks))
	processes, reporting := r.backend.(processRunner)
	run := func(i int) {
		if reporting {
			result, err := processes.Run(ctx, checks[i].request, checks[i].prepared.Vars)
			outcomes[i] = outcome{diagnostics: result.Diagnostics, sources: result.Sources, directory: result.Directory, err: err}
			return
		}
		outcomes[i] = r.checkGranted(ctx, checks[i])
	}
	if !reporting || !processes.Concurrent() || len(checks) < 2 {
		for i := range checks {
			run(i)
		}
		return outcomes
	}
	var group errgroup.Group
	group.SetLimit(runtime.GOMAXPROCS(0))
	for i := range checks {
		group.Go(func() error {
			run(i)
			return nil
		})
	}
	// Each check records its own error; the group reports none.
	_ = group.Wait()
	return outcomes
}

// findings maps one check's diagnostics to findings in the workflow.
func (r runner) findings(c check, script tools.Script, result outcome) ([]tools.Finding, error) {
	var out []tools.Finding
	var failures error
	if len(result.sources) != 0 {
		failures = errors.Join(failures, r.recordSources(script, result.sources, result.directory))
	}
	files := map[string]reportedFile{}
	for _, d := range result.diagnostics {
		if d.Source != "" {
			if !r.options.findings.reportedSources {
				failures = errors.Join(failures, fmt.Errorf("tool %s reported a finding in %q but does not declare reported sources", r.descriptor.Name, d.Source))
				continue
			}
			// Template effects describe the masked snippet. A file the engine
			// read has no template expressions, so it carries none.
			if d.TemplateEffect != "" && d.TemplateEffect != analyzer.TemplateEffectNone {
				failures = errors.Join(failures, fmt.Errorf("tool %s returned template effect %q for reported source %q", r.descriptor.Name, d.TemplateEffect, d.Source))
				continue
			}
			out = append(out, r.reportedFinding(c.index, script, d, result, files))
			continue
		}
		if r.templateContract == analyzer.TemplateEffectsContract && !d.TemplateEffect.Valid() ||
			r.templateContract != analyzer.TemplateEffectsContract && d.TemplateEffect != "" {
			failures = errors.Join(failures, fmt.Errorf("tool %s returned an undeclared template effect %q", r.descriptor.Name, d.TemplateEffect))
			continue
		}
		if d.Filename != "" && d.Filename != c.request.Filename {
			failures = errors.Join(failures, fmt.Errorf("tool %s returned an unrelated source %q", r.descriptor.Name, d.Filename))
			continue
		}
		if len(d.Fixes) != 0 && !r.options.findings.fixes {
			failures = errors.Join(failures, fmt.Errorf("tool %s returned fixes but does not declare them", r.descriptor.Name))
			continue
		}
		if c.prepared.AcceptDiagnostic != nil && !c.prepared.AcceptDiagnostic(d) {
			continue
		}
		finding, err := r.scriptFinding(c, script, d)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		if finding != nil {
			out = append(out, *finding)
		}
	}
	return out, failures
}

// scriptFinding maps a snippet diagnostic through the prepared line map. A
// diagnostic on a generated directive is a configuration error; one on a
// generated startup line is dropped unless its severity is error. An
// unmapped start falls back to the script key; an unmapped or reversed end is
// dropped.
func (r runner) scriptFinding(c check, script tools.Script, d analyzer.ScriptDiagnostic) (*tools.Finding, error) {
	row, endRow := d.Location.Row, d.EndLocation.Row
	hasEnd := d.EndLocation != (analyzer.ScriptPosition{})
	if lines := c.prepared.LineMap; lines != nil {
		start := lines(d.Location.Row)
		switch start.Origin {
		case profile.LineScript:
			row = start.Line
		case profile.LineStartup:
			if r.severity(d.Severity) != provider.SeverityError {
				r.debug("%s: dropped %s on the generated startup line: %s", r.descriptor.Name, d.Code, d.Message)
				return nil, nil
			}
			return nil, r.configurationError(d)
		case profile.LineDirective:
			return nil, r.configurationError(d)
		}
		if hasEnd {
			end := lines(d.EndLocation.Row)
			endRow, hasEnd = end.Line, end.Origin == profile.LineScript
		}
	}
	start, ok := script.Source.Position(row, d.Location.Column)
	if !ok {
		start, hasEnd = *script.Key, false
	}
	var end diagnostic.Position
	if hasEnd {
		position, endOK := script.Source.EndPos(endRow, d.EndLocation.Column)
		if endOK && (position.Line > start.Line || position.Line == start.Line && position.Col >= start.Col) {
			end = diagnostic.Position{Line: position.Line, Column: position.Col}
		} else {
			r.debug("%s: dropped the unmapped or reversed end of %s at %d:%d", r.descriptor.Name, d.Code, row, d.Location.Column)
		}
	}
	finding := tools.Finding{Rule: r.descriptor.Name, Path: script.Path, Message: r.message(d, row, d.Location.Column), Code: d.Code, Severity: string(r.severity(d.Severity)), DocumentationURL: d.URL, Start: diagnostic.Position{Line: start.Line, Column: start.Col}, End: end, Script: c.index, Content: script.Content}
	for _, fix := range d.Fixes {
		if mapped, ok := scriptFix(script.Source, c.prepared, fix); ok {
			finding.Fixes = append(finding.Fixes, mapped)
		}
	}
	return &finding, nil
}

// configurationError reports a diagnostic on a line preparation generated
// from the tool's configuration.
func (r runner) configurationError(d analyzer.ScriptDiagnostic) error {
	if d.Code == "" {
		return fmt.Errorf("tools.%s.config: %s", r.descriptor.Name, d.Message)
	}
	return fmt.Errorf("tools.%s.config: %s: %s", r.descriptor.Name, d.Code, d.Message)
}

// message formats a finding message from the manifest's template. Row and
// column are script coordinates after line mapping.
func (r runner) message(d analyzer.ScriptDiagnostic, row, column int) string {
	if r.options.findings.messageTemplate == "" {
		return d.Message
	}
	return strings.NewReplacer("{code}", d.Code, "{severity}", d.Severity, "{row}", strconv.Itoa(row), "{column}", strconv.Itoa(column), "{message}", d.Message).Replace(r.options.findings.messageTemplate)
}

// severity maps an engine severity to a host severity through the
// manifest's table, logging a severity the table does not list.
func (r runner) severity(severity string) provider.Severity {
	mapped, listed := provider.Capabilities{Severities: r.options.findings.severities}.HostSeverity(severity)
	if !listed {
		r.debug("%s: mapped unlisted severity %q to %s", r.descriptor.Name, severity, mapped)
	}
	return mapped
}

func (r runner) debug(format string, args ...any) {
	if r.context.Debug != nil {
		r.context.Debug(format, args...)
	}
}

// filename is the virtual snippet name of a request.
func (r runner) filename(script tools.Script) string {
	if script.Input != nil {
		return inputFilename(*script.Input, strings.TrimPrefix(path.Ext(r.options.filename), "."))
	}
	if r.options.filename != "" {
		return r.options.filename
	}
	return "actionlint-script"
}

// inputFilename names an action-input snippet. The extension comes from the
// tool's snippet filename; engines without one name it from the language.
func inputFilename(site analyzer.ScriptInput, extension string) string {
	if extension == "" {
		extension = site.Language
		switch extension {
		case "python":
			extension = "py"
		case "javascript":
			extension = "js"
		case "typescript":
			extension = "ts"
		}
	}
	return "actionlint-input/" + site.Language + "/" + site.Action + "/" + site.Input + "." + extension
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
