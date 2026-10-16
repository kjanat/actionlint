package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/ruff"
)

// ruffAnalyzer consumes prepared scripts. It has no traversal or diagnostic state.
type ruffAnalyzer struct {
	checker   *ruff.Checker
	forConfig func(*Config) (*ruff.Checker, error)
}

func newRuffAnalyzer(c ruleContext) (scriptAnalyzer, error) {
	checker, err := newRuffChecker(c)
	if err != nil {
		options := projectToolOptions("ruff", c.ruff, c.ruffOptions, c.config, c.workingDir)
		if options == nil || !options.Optional {
			return nil, err
		}
	}
	return &ruffAnalyzer{checker: checker, forConfig: func(cfg *Config) (*ruff.Checker, error) {
		if cfg == c.config {
			return checker, nil
		}
		invocation := c
		invocation.config = cfg
		checker, err := newRuffChecker(invocation)
		options := projectToolOptions("ruff", c.ruff, c.ruffOptions, cfg, c.workingDir)
		if err != nil && options != nil && options.Optional {
			return nil, nil
		}
		return checker, err
	}}, nil
}

func newRuffChecker(c ruleContext) (*ruff.Checker, error) {
	if c.ruffBackend != nil {
		return ruff.NewBackend(c.process.ctx, c.ruffBackend, ruffExpressionEnd), nil
	}
	c.ruffOptions = projectToolOptions("ruff", c.ruff, c.ruffOptions, c.config, c.workingDir)
	cmd, err := c.process.configuredCommandRunner(c.ruff, c.ruffOptions, false)
	if err != nil {
		return nil, fmt.Errorf("could not initialize Ruff: %w", err)
	}
	cmd.maxExitCode = 1
	cmd.unsetEnv = ruff.UnsetEnvironment()
	backend, prefix := ruff.NewProcessBackend(func(ctx context.Context, args []string, source string) ([]byte, error) {
		if err := cmd.proc.sema.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer cmd.proc.sema.Release(1)
		execution := cmdExecution{
			cmd: cmd.exe, args: slices.Concat(cmd.launcherArgs, cmd.args, args), stdin: source,
			dir: cmd.dir, env: cmd.env, unsetEnv: cmd.unsetEnv, maxExitCode: 1,
		}
		return execution.run(ctx)
	}, cmd.exe, cmd.args, cmd.dir)
	cmd.args = prefix
	if c.ruffOptions != nil && c.ruffOptions.Optional {
		compatibility := c.ruffCompatibility
		if compatibility == nil {
			compatibility = &ruff.Compatibility{}
		}
		backend.OptionalVersion(compatibility, c.ruffWarning)
	}
	checker := ruff.NewBackend(c.process.ctx, backend, ruffExpressionEnd)
	return checker, nil
}

func ruffExpressionEnd(src string) (int, bool) {
	lexer := exprlang.NewLexer(src)
	_, err := exprlang.NewParser().Parse(lexer)
	return lexer.Offset(), err == nil
}

func (r *ruffAnalyzer) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*Error, error) {
	scripts := batch.scripts
	// Each callback writes only its script's slot. Wait before reading any slots.
	results := make([][]*Error, len(scripts))
	var failure error
	checkers := map[*Config]*ruff.Checker{}
	pending := []*ruff.Checker{r.checker}
	for i, script := range scripts {
		if err := ctx.Err(); err != nil {
			failure = err
			break
		}
		if diagnosticLevel(script.config, "ruff") == "off" {
			continue
		}
		config := RuffToolConfig{}
		if script.config != nil {
			config = script.config.Tools.Ruff
		}
		if config.Enabled != nil && !*config.Enabled {
			continue
		}
		run := script.run
		checker := r.checker
		if r.forConfig != nil {
			var ok bool
			checker, ok = checkers[script.config]
			if !ok {
				var err error
				checker, err = r.forConfig(script.config)
				if err != nil {
					failure = errors.Join(failure, err)
					continue
				}
				checkers[script.config] = checker
				if checker != nil && checker != r.checker {
					pending = append(pending, checker)
				}
			}
		}
		if checker == nil {
			continue
		}
		err := checker.Check(run.Run.Value, script.shell, run.RunPos.String(), config, func(d ruff.Diagnostic) {
			pos, mapped := run.Source.Position(d.Location.Row, d.Location.Column)
			end, endMapped := run.Source.EndPos(d.EndLocation.Row, d.EndLocation.Column)
			if !mapped || !endMapped {
				pos, end = run.RunPos, nil
			}
			finding := errorfAt(pos, "ruff", "Ruff reported issue in this script: %s: %s", d.Code, d.Message)
			finding.endPosition = end
			if end != nil && end.Line == pos.Line && end.Col > pos.Col {
				finding.endColumn = end.Col - 1
			}
			finding.code, finding.severity = d.Code, d.Severity
			if finding.severity == "" {
				finding.severity = "error"
			}
			finding.ignoreOrigin = script.origin
			finding.Filepath, finding.source = script.path, script.content
			results[i] = append(results[i], finding)
		})
		if err != nil {
			failure = err
			break
		}
	}
	failure = errors.Join(failure, ctx.Err())
	for _, checker := range pending {
		if checker != nil {
			failure = errors.Join(failure, checker.Wait())
		}
	}
	return slices.Concat(results...), failure
}
