package engine

import (
	"context"
	"errors"
	"slices"
)

// shellcheckAnalyzer owns execution; traversal supplies resolved invocation facts.
type shellcheckAnalyzer struct {
	cmd      *externalCommand
	settings *ShellcheckSettings
	debug    func(string, ...any)
	embedded bool
}

func newShellcheckAnalyzer(c ruleContext) (scriptAnalyzer, error) {
	if c.shellcheckRunner != nil {
		externalSources := false
		return &shellcheckAnalyzer{
			cmd:      &externalCommand{exe: "shellcheck", runner: c.shellcheckRunner, env: []string{"SHELLCHECK_OPTS="}},
			settings: &ShellcheckSettings{Config: ShellcheckRCDisabled, ExternalSources: &externalSources},
			debug:    c.debug, embedded: true,
		}, nil
	}
	cmd, err := c.process.configuredCommandRunner(c.shellcheck, c.shellcheckOptions, false)
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
	for _, scope := range batch.scopes {
		if err := ctx.Err(); err != nil {
			failure = err
			break
		}
		run := &shellcheckRun{cmd: analyzer.cmd, config: analyzer.settings, cfg: scope.config, paths: scope.paths, actionPath: scope.actionPath, debug: analyzer.debug}
		if analyzer.embedded {
			run.paths.analysis = "/"
		}
		if err := run.prepareConfigPath(); err != nil {
			if !scope.composite && errors.Is(err, errConfigActionPathUnavailable) {
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
		if err := run.runShellcheck(script.run.Run.Value, script.run.source, script.execution.shell, script.execution.directory, script.run.RunPos, script.run.Run.Pos); err != nil {
			failure = errors.Join(failure, err)
			break
		}
	}
	// Joining is independent of visitor success, cancellation and configuration errors.
	failure = errors.Join(failure, analyzer.cmd.wait(), ctx.Err())
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
