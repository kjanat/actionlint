package engine

import (
	"actionlint.kjanat.dev/internal/repoconfig"
	"context"
	"errors"
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
	forConfig func(*Config) (*externalCommand, error)
}

func newShellcheckAnalyzer(c ruleContext) (scriptAnalyzer, error) {
	if c.shellcheckBackend != nil {
		return &shellcheckAnalyzer{backend: c.shellcheckBackend, debug: c.debug}, nil
	}
	if c.shellcheckRunner != nil {
		externalSources := false
		return &shellcheckAnalyzer{
			cmd:      &externalCommand{exe: "shellcheck", runner: c.shellcheckRunner, env: []string{"SHELLCHECK_OPTS="}},
			settings: &ShellcheckSettings{Config: ShellcheckRCDisabled, ExternalSources: &externalSources},
			debug:    c.debug, embedded: true,
		}, nil
	}
	commandFor := func(cfg *Config) (*externalCommand, error) {
		options := projectToolOptions("shellcheck", c.shellcheck, c.shellcheckOptions, cfg, c.workingDir)
		return c.process.configuredCommandRunner(c.shellcheck, options, false)
	}
	cmd, err := commandFor(c.config)
	if err != nil {
		return nil, err
	}
	return &shellcheckAnalyzer{cmd: cmd, settings: c.shellcheckSettings, debug: c.debug, forConfig: func(cfg *Config) (*externalCommand, error) {
		if cfg == c.config {
			return cmd, nil
		}
		return commandFor(cfg)
	}}, nil
}

func (analyzer *shellcheckAnalyzer) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*Error, error) {
	prepared := make(map[*scriptScope]*shellcheckRun, len(batch.scopes))
	var failure, deferred error
	var deferredScope *scriptScope
	compositePrepared := false
	commands := map[*Config]*externalCommand{}
	pending := []*externalCommand{analyzer.cmd}
	for _, scope := range batch.scopes {
		if err := ctx.Err(); err != nil {
			failure = err
			break
		}
		if scope.config != nil && (diagnosticLevel(scope.config, "shellcheck") == "off" || scope.config.Tools.Shellcheck.Enabled != nil && !*scope.config.Tools.Shellcheck.Enabled) {
			continue
		}
		if analyzer.backend != nil {
			run := &shellcheckRun{backend: analyzer.backend, cfg: scope.config, paths: scope.paths, actionPath: scope.actionPath, debug: analyzer.debug, rcArgs: []string{}}
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
		cmd := analyzer.cmd
		if analyzer.forConfig != nil {
			var ok bool
			cmd, ok = commands[scope.config]
			if !ok {
				var err error
				cmd, err = analyzer.forConfig(scope.config)
				if err != nil {
					failure = errors.Join(failure, err)
					continue
				}
				commands[scope.config] = cmd
				if cmd != analyzer.cmd {
					pending = append(pending, cmd)
				}
			}
		}
		run := &shellcheckRun{cmd: cmd, config: analyzer.settings, cfg: scope.config, paths: scope.paths, actionPath: scope.actionPath, debug: analyzer.debug}
		if analyzer.embedded {
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
