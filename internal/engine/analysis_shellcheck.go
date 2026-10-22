package engine

import (
	"actionlint.kjanat.dev/internal/repoconfig"
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/analyzerhost"
)

// shellcheckAnalyzer owns execution; traversal supplies resolved invocation facts.
type shellcheckAnalyzer struct {
	backend   analyzer.ScriptBackend
	cmd       *externalCommand
	settings  *ShellcheckSettings
	debug     func(string, ...any)
	embedded  bool
	forConfig func(*Config) (*shellcheckAnalyzer, error)
}

func newShellcheckAnalyzer(c ruleContext) (scriptAnalyzer, error) {
	initial, err := newShellcheckInvocation(c)
	if err != nil {
		return nil, err
	}
	if initial == nil {
		// Retain per-composite selection even when the calling workflow has no
		// engine. The empty invocation never executes a native command.
		initial = &shellcheckAnalyzer{debug: c.debug}
	}
	if c.shellcheckBackend == nil && c.shellcheckRunner == nil {
		initial.forConfig = func(cfg *Config) (*shellcheckAnalyzer, error) {
			if cfg == c.config {
				return initial, nil
			}
			invocation := c
			invocation.config = cfg
			return newShellcheckInvocation(invocation)
		}
	}
	return initial, nil
}

func newShellcheckInvocation(c ruleContext) (*shellcheckAnalyzer, error) {
	if c.shellcheckBackend != nil {
		return &shellcheckAnalyzer{backend: c.shellcheckBackend, debug: c.debug}, nil
	}
	if engine, err := c.configuredScriptEngine("shellcheck", c.shellcheck, c.shellcheckOptions); engine != nil || err != nil {
		if err != nil {
			return nil, fmt.Errorf("could not initialize ShellCheck: %w", err)
		}
		return &shellcheckAnalyzer{backend: engine, debug: c.debug}, nil
	}
	if c.shellcheckRunner != nil {
		externalSources := false
		return &shellcheckAnalyzer{
			cmd:      &externalCommand{exe: "shellcheck", runner: c.shellcheckRunner, env: []string{"SHELLCHECK_OPTS="}},
			settings: &ShellcheckSettings{Config: ShellcheckRCDisabled, ExternalSources: &externalSources},
			debug:    c.debug, embedded: true,
		}, nil
	}
	if !externalCommandEnabled(c.shellcheck, c.shellcheckOptions) {
		return nil, nil
	}
	commandFor := func(cfg *Config) (*externalCommand, error) {
		if err := c.requireConfigProcessOptIn("shellcheck", c.shellcheck, c.shellcheckOptions, cfg); err != nil {
			return nil, err
		}
		options := projectToolOptions("shellcheck", c.shellcheck, c.shellcheckOptions, cfg, c.workingDir)
		return c.process.configuredCommandRunner(c.shellcheck, options, false)
	}
	cmd, err := commandFor(c.config)
	if err != nil {
		return nil, err
	}
	return &shellcheckAnalyzer{cmd: cmd, settings: c.shellcheckSettings, debug: c.debug}, nil
}

func (analyzer *shellcheckAnalyzer) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*Error, error) {
	prepared := make(map[*scriptScope]*shellcheckRun, len(batch.scopes))
	var failure, deferred error
	var deferredScope *scriptScope
	compositePrepared := false
	invocations := map[*Config]*shellcheckAnalyzer{}
	pending := []*externalCommand{analyzer.cmd}
	for _, scope := range batch.scopes {
		if err := ctx.Err(); err != nil {
			failure = errors.Join(failure, err)
			break
		}
		if scope.config != nil && (diagnosticLevel(scope.config, "shellcheck") == "off" || scope.config.Tools.Shellcheck.Enabled != nil && !*scope.config.Tools.Shellcheck.Enabled) {
			continue
		}
		active := analyzer
		if analyzer.forConfig != nil {
			var ok bool
			active, ok = invocations[scope.config]
			if !ok {
				var err error
				active, err = analyzer.forConfig(scope.config)
				if err != nil {
					failure = errors.Join(failure, err)
					continue
				}
				invocations[scope.config] = active
				if active != nil && active.cmd != nil && active.cmd != analyzer.cmd {
					pending = append(pending, active.cmd)
				}
			}
		}
		if active == nil || active.backend == nil && active.cmd == nil {
			continue
		}
		if active.backend != nil {
			run := &shellcheckRun{backend: active.backend, cfg: scope.config, paths: scope.paths, actionPath: scope.actionPath, debug: analyzer.debug, rcArgs: []string{}}
			if scope.config != nil && scope.config.Tools.Shellcheck.Config.Path() != "" {
				failure = errors.Join(failure, errors.New("ShellCheck backend needs inline configuration; rc-file access requires a process backend"))
				continue
			}
			if err := run.prepareInlineConfig(); err != nil {
				failure = errors.Join(failure, err)
				continue
			}
			if run.inlineConfig != nil && (len(run.inlineConfig.SourcePath) != 0 || run.inlineConfig.ExternalSources != nil && *run.inlineConfig.ExternalSources) {
				failure = errors.Join(failure, errors.New("ShellCheck backend does not provide external source access"))
				continue
			}
			prepared[scope] = run
			continue
		}
		run := &shellcheckRun{cmd: active.cmd, config: active.settings, cfg: scope.config, paths: scope.paths, actionPath: scope.actionPath, debug: active.debug}
		if active.embedded {
			run.paths.analysis = "/"
		}
		if err := run.prepareConfigPath(); err != nil {
			if !scope.composite && errors.Is(err, repoconfig.ErrActionPathUnavailable) {
				deferred, deferredScope = err, scope
				continue
			}
			failure = errors.Join(failure, err)
			continue
		}
		prepared[scope] = run
		compositePrepared = compositePrepared || scope.composite
	}
	if deferred != nil && !compositePrepared {
		failure = errors.Join(failure, deferred)
	}
	runs := make([]*shellcheckRun, len(batch.scripts))
	for i, script := range batch.scripts {
		if err := ctx.Err(); err != nil {
			failure = errors.Join(failure, err)
			break
		}
		base := prepared[script.scope]
		if base == nil {
			if script.scope == deferredScope {
				failure = errors.Join(failure, deferred)
				deferredScope = nil
			}
			continue
		}
		run := *base
		run.ignoreOrigin = script.origin
		runs[i] = &run
		if err := run.runShellcheck(script.run.Run.Value, script.run.Source, script.execution.shell, script.execution.directory, script.run.RunPos, script.run.Run.Pos); err != nil {
			failure = errors.Join(failure, err)
			break
		}
	}
	// Joining is independent of visitor success, cancellation and configuration errors.
	failure = errors.Join(failure, ctx.Err())
	for _, cmd := range pending {
		if cmd != nil {
			failure = errors.Join(failure, cmd.wait())
		}
	}
	var tasks []analyzerhost.Task[*Error]
	for _, run := range runs {
		if run != nil && run.pending != nil {
			tasks = append(tasks, run.pending)
		}
	}
	_, backendError := analyzerhost.RunTasks(ctx, tasks)
	failure = errors.Join(failure, backendError)
	results := make([][]*Error, len(runs))
	for i, run := range runs {
		if run == nil {
			continue
		}
		script := batch.scripts[i]
		for _, finding := range run.errs {
			if finding.Filepath == "" {
				finding.Filepath, finding.source = script.path, script.content
			}
			if origin := finding.suppressionOrigin; origin != nil && origin.path == "" {
				origin.path = script.path
			}
		}
		results[i] = run.errs
	}
	return slices.Concat(results...), failure
}
