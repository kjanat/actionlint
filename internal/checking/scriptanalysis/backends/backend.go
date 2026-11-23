// Package backends adapts the shared snippet protocol to registered script tools.
package backends

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

	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/executor"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
	"actionlint.kjanat.dev/v2/internal/externallinters/preparation/mask"
	"actionlint.kjanat.dev/v2/internal/externallinters/process"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
	"actionlint.kjanat.dev/v2/internal/results"
	"golang.org/x/sync/errgroup"
)

func Register(descriptor rule.Descriptor, shells []string, backend scriptengine.Backend) (contract.Registration, error) {
	return RegisterEngine(scriptengine.Descriptor{Rule: descriptor, Shells: shells}, backend)
}

func RegisterEngine(descriptor scriptengine.Descriptor, backend scriptengine.Backend) (contract.Registration, error) {
	return registerEngine(descriptor, backend, engineOptions{})
}

type engineOptions struct {
	enabled func(contract.Context) bool
	prepare func(context.Context, scriptanalysis.Unit) (preparation.Prepared, error)
	// With batch set, run scripts reach the preparation even when no shell selects them.
	batch    func(context.Context, scriptanalysis.Batch) (func(context.Context, scriptanalysis.Unit) (preparation.Prepared, error), error)
	filename string
	findings findingCapabilities
}

type findingCapabilities struct {
	fixes           bool
	reportedSources bool
	messageTemplate string
	// An unmapped severity becomes a warning.
	severities map[string]severity.Severity
}

func registerEngine(descriptor scriptengine.Descriptor, backend scriptengine.Backend, options engineOptions) (contract.Registration, error) {
	descriptor = descriptor.Clone()
	if backend == nil {
		return contract.Registration{}, errors.New("script engine requires a rule, shell selectors and a backend")
	}
	if err := checkinputs.ValidateScriptInputs(descriptor.ScriptInputs); err != nil {
		return contract.Registration{}, err
	}
	if descriptor.Rule.Name == "" || len(descriptor.Shells)+len(descriptor.ScriptInputs) == 0 && options.batch == nil {
		return contract.Registration{}, errors.New("script engine requires a rule and script selectors")
	}
	selectors, err := shellCatalogue(descriptor.Rule, descriptor.Shells)
	if err != nil {
		return contract.Registration{}, err
	}
	inputs := make(map[checkinputs.ScriptInput]struct{}, len(descriptor.ScriptInputs))
	for _, input := range descriptor.ScriptInputs {
		inputs[input] = struct{}{}
	}
	options.findings.fixes = options.findings.fixes || descriptor.Fixes
	options.findings.reportedSources = options.findings.reportedSources || descriptor.ReportedSources
	directory := slices.Contains(descriptor.Resources, checkinputs.ToolDirectory)
	return contract.Registration{Descriptor: descriptor.Rule, Scope: rule.SourceConfiguration, SupportsActions: true, ScriptInputs: append([]checkinputs.ScriptInput(nil), descriptor.ScriptInputs...), RequiresExecution: len(descriptor.Shells) != 0 || directory, Build: func(c contract.Context) (contract.Runner, error) {
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
	inputs           map[checkinputs.ScriptInput]struct{}
	backend          scriptengine.Backend
	context          contract.Context
	options          engineOptions
	directory        bool
}

// processRunner may run checks at once, and each check holds a slot of the shared process pool.
type processRunner interface {
	Run(context.Context, scriptengine.Request, map[string]string) (process.DecodedCheck, error)
	Concurrent() bool
}

func (r runner) scriptEnabled(cfg *configuration.Config) bool {
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

type check struct {
	index    int
	prepared preparation.Prepared
	request  scriptengine.Request
	scope    *scriptanalysis.Scope
}

// Reported sources missing from copies are unreadable.
type outcome struct {
	skipped     bool
	diagnostics []scriptengine.Diagnostic
	sources     []string
	directory   string
	copies      map[string]reportedFile
	err         error
}

func (r runner) Analyze(ctx context.Context, batch scriptanalysis.Batch) ([]contract.ToolFinding, error) {
	checks, failures := r.prepare(ctx, batch)
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(failures, err)
	}
	outcomes := r.check(ctx, checks)
	var out []contract.ToolFinding
	for i, c := range checks {
		findings, err := r.findings(c, batch.Scripts[c.index], outcomes[i])
		out = append(out, findings...)
		failures = errors.Join(failures, outcomes[i].err, err)
		if !outcomes[i].skipped && outcomes[i].err == nil && err == nil && r.context.Completed != nil {
			r.context.Completed(r.descriptor.Name)
		}
	}
	return out, failures
}

func (r runner) prepare(ctx context.Context, batch scriptanalysis.Batch) ([]check, error) {
	var checks []check
	var failures error
	_, reporting := r.backend.(processRunner)
	prepare := r.options.prepare
	if r.options.batch != nil {
		var err error
		prepare, err = r.options.batch(ctx, batch)
		failures = errors.Join(failures, err)
	}
	for i, script := range batch.Scripts {
		if err := ctx.Err(); err != nil {
			return checks, failures
		}
		if !r.scriptEnabled(script.Config()) || script.Config().RuleSet().DiagnosticLevel(script.Config(), r.descriptor.Name) == "off" {
			continue
		}
		if script.Input != nil {
			if _, supported := r.inputs[script.Input.Site]; !supported {
				continue
			}
		} else if len(r.shells) != 0 || r.options.batch == nil {
			shell := scriptShell(script)
			if _, supported := r.shells[shell]; !supported {
				continue
			}
		}
		var options json.RawMessage
		var prepared preparation.Prepared
		if prepare != nil {
			var err error
			prepared, err = prepare(ctx, script)
			if err != nil {
				failures = errors.Join(failures, err)
				continue
			}
			options = prepared.Config
		} else {
			prepared.Source = mask.Expressions(script.Text())
			if script.Config() != nil {
				settings, err := script.Config().Lint.Rules.RuleOptions(r.descriptor.Name)
				if err != nil {
					return checks, errors.Join(failures, err)
				}
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
		if err := executor.ValidateOptions(r.descriptor.OptionsSchema, options); err != nil {
			failures = errors.Join(failures, fmt.Errorf("tool %s options: %w", r.descriptor.Name, err))
			continue
		}
		if validator, ok := r.backend.(scriptengine.ConfigValidator); ok {
			if err := validator.ValidateConfig(ctx, options); err != nil {
				failures = errors.Join(failures, err)
				continue
			}
		}
		if validator, ok := r.backend.(scriptengine.TemplateValidator); ok && prepared.TemplateContract != "" {
			available, err := validator.ValidateTemplates(ctx)
			if err != nil || !available {
				failures = errors.Join(failures, err)
				continue
			}
		}
		request := scriptengine.Request{Source: prepared.Source, Filename: r.filename(script), Config: options, Excluded: prepared.Excluded}
		request.Environment = script.Environment
		if directory, known := script.WorkingDirectory(); (reporting || r.directory) && known {
			request.Directory = directory
		}
		checks = append(checks, check{index: i, prepared: prepared, request: request, scope: script.Scope})
	}
	return checks, failures
}

func (r runner) check(ctx context.Context, checks []check) []outcome {
	type job struct {
		check int
		run   check
	}
	var jobs []job
	for i, c := range checks {
		for _, run := range c.runs() {
			jobs = append(jobs, job{check: i, run: run})
		}
	}
	results := make([]outcome, len(jobs))
	processes, reporting := r.backend.(processRunner)
	run := func(i int) {
		if reporting {
			result, err := processes.Run(ctx, jobs[i].run.request, jobs[i].run.prepared.Vars)
			results[i] = outcome{diagnostics: result.Diagnostics, sources: result.Sources, directory: result.Directory, skipped: result.Skipped, err: err}
			return
		}
		results[i] = r.checkGranted(ctx, jobs[i].run)
	}
	if !reporting || !processes.Concurrent() || len(jobs) < 2 {
		for i := range jobs {
			run(i)
		}
	} else {
		var group errgroup.Group
		group.SetLimit(runtime.GOMAXPROCS(0))
		for i := range jobs {
			group.Go(func() error {
				run(i)
				return nil
			})
		}
		// Each check records its own error; the group reports none.
		_ = group.Wait()
	}
	grouped := make([][]outcome, len(checks))
	for i, job := range jobs {
		grouped[job.check] = append(grouped[job.check], results[i])
	}
	outcomes := make([]outcome, len(checks))
	for i, c := range checks {
		outcomes[i] = mergeVariants(c.prepared, grouped[i])
	}
	return outcomes
}

func (c check) runs() []check {
	if len(c.prepared.Variants) == 0 {
		return []check{c}
	}
	runs := make([]check, len(c.prepared.Variants))
	for i, variant := range c.prepared.Variants {
		runs[i] = c
		runs[i].request.Source = variant.Source
	}
	return runs
}

func (r runner) findings(c check, script scriptanalysis.Unit, result outcome) ([]contract.ToolFinding, error) {
	var out []contract.ToolFinding
	var failures error
	if len(result.sources) != 0 {
		failures = errors.Join(failures, r.recordSources(script, result.sources, result.directory))
	}
	files := map[string]reportedFile{}
	for _, d := range result.diagnostics {
		if d.Source != "" && c.prepared.ConfigFile != "" && sourcePath(d.Source, result.directory) == sourcePath(c.prepared.ConfigFile, result.directory) {
			failures = errors.Join(failures, r.configurationError(d))
			continue
		}
		if d.Source != "" {
			if !r.options.findings.reportedSources {
				failures = errors.Join(failures, fmt.Errorf("tool %s reported a finding in %q but does not declare reported sources", r.descriptor.Name, d.Source))
				continue
			}
			if d.TemplateEffect != "" && d.TemplateEffect != scriptengine.TemplateEffectNone {
				failures = errors.Join(failures, fmt.Errorf("tool %s returned template effect %q for reported source %q", r.descriptor.Name, d.TemplateEffect, d.Source))
				continue
			}
			out = append(out, r.reportedFinding(c.index, script, d, result, files))
			continue
		}
		if r.templateContract == scriptengine.TemplateEffectsContract && !d.TemplateEffect.Valid() ||
			r.templateContract != scriptengine.TemplateEffectsContract && d.TemplateEffect != "" {
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

func (r runner) scriptFinding(c check, script scriptanalysis.Unit, d scriptengine.Diagnostic) (*contract.ToolFinding, error) {
	row, endRow := d.Location.Line, d.EndLocation.Line
	hasEnd := d.EndLocation != (sourcetext.Position{})
	if lines := c.prepared.LineMap; lines != nil {
		start := lines(d.Location.Line)
		switch start.Origin {
		case preparation.LineScript:
			row = start.Line
		case preparation.LineStartup:
			if r.severity(d.Severity) != severity.Error {
				r.debug("%s: dropped %s on the generated startup line: %s", r.descriptor.Name, d.Code, d.Message)
				return nil, nil
			}
			return nil, r.configurationError(d)
		case preparation.LineDirective:
			return nil, r.configurationError(d)
		}
		if hasEnd {
			end := lines(d.EndLocation.Line)
			endRow, hasEnd = end.Line, end.Origin == preparation.LineScript
		}
	}
	start, ok := script.Source().Position(row, d.Location.Column)
	if !ok {
		start, hasEnd = script.Key(), false
	}
	var end sourcetext.Position
	if hasEnd {
		position, endOK := script.Source().EndPos(endRow, d.EndLocation.Column)
		if endOK && (position.Line > start.Line || position.Line == start.Line && position.Column >= start.Column) {
			end = *position
		} else {
			r.debug("%s: dropped the unmapped or reversed end of %s at %d:%d", r.descriptor.Name, d.Code, row, d.Location.Column)
		}
	}
	finding := contract.ToolFinding{Rule: r.descriptor.Name, Path: script.Path, Message: r.message(d, row, d.Location.Column), Code: d.Code, Severity: r.severity(d.Severity), DocumentationURL: d.URL, Start: *start, End: end, Script: c.index, Source: results.DiagnosticSource{Content: script.Content}}
	for _, fix := range d.Fixes {
		if mapped, ok := scriptFix(script.Source(), c.prepared, fix); ok {
			finding.Fixes = append(finding.Fixes, mapped)
		}
	}
	return &finding, nil
}

func (r runner) configurationError(d scriptengine.Diagnostic) error {
	if d.Code == "" {
		return fmt.Errorf("tools.%s.config: %s", r.descriptor.Name, d.Message)
	}
	return fmt.Errorf("tools.%s.config: %s: %s", r.descriptor.Name, d.Code, d.Message)
}

func (r runner) message(d scriptengine.Diagnostic, row, column int) string {
	if r.options.findings.messageTemplate == "" {
		return d.Message
	}
	return strings.NewReplacer("{code}", d.Code, "{severity}", d.Severity, "{row}", strconv.Itoa(row), "{column}", strconv.Itoa(column), "{message}", d.Message).Replace(r.options.findings.messageTemplate)
}

func (r runner) severity(tool string) severity.Severity {
	mapped, listed := toolmanifest.Capabilities{Severities: r.options.findings.severities}.HostSeverity(tool)
	if !listed {
		r.debug("%s: mapped unlisted severity %q to %s", r.descriptor.Name, tool, mapped)
	}
	return mapped
}

func (r runner) debug(format string, args ...any) {
	if r.context.Debug != nil {
		r.context.Debug(format, args...)
	}
}

func (r runner) filename(script scriptanalysis.Unit) string {
	if script.Input != nil {
		return inputFilename(script.Input.Site, strings.TrimPrefix(path.Ext(r.options.filename), "."))
	}
	if r.options.filename != "" {
		return r.options.filename
	}
	return "actionlint-script"
}

func inputFilename(site checkinputs.ScriptInput, extension string) string {
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

func scriptShell(script scriptanalysis.Unit) string {
	shell := script.RuntimeShell()
	if !shell.Known() && script.Shell != nil {
		shell.Name = *script.Shell
	}
	registry := script.Interpreters
	if registry == nil {
		registry = commands.BuiltinShells()
	}
	invocation := registry.Analyze(shell.Name, shell.Implicit)
	if invocation.Status == commands.Valid && invocation.Input == "file" && invocation.Language != "" {
		return invocation.Language
	}
	return strings.TrimSuffix(strings.ToLower(commands.ExecutableName(invocation.Program)), ".exe")
}
