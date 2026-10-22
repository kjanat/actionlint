package ruff

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/config"
	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools"
	"actionlint.kjanat.dev/internal/tools/selection"
)

// ruffAnalyzer consumes prepared scripts. It has no traversal or diagnostic state.
type ruffAnalyzer struct {
	checker   *Checker
	forConfig func(*config.Config) (*Checker, error)
}

func newRuffAnalyzer(c tools.Context, o Options, compatibility *Compatibility) (*ruffAnalyzer, error) {
	checker, err := newRuffChecker(c, o, compatibility)
	if err != nil {
		options := selection.ProcessOptions("ruff", o.Command, o.Process, c.Config, c.WorkingDir)
		if options == nil || !options.Optional || selection.Module("ruff", o.Command, o.Process, c.Config) != nil {
			return nil, err
		}
	}
	return &ruffAnalyzer{checker: checker, forConfig: func(cfg *config.Config) (*Checker, error) {
		if cfg == c.Config {
			return checker, nil
		}
		invocation := c
		invocation.Config = cfg
		checker, err := newRuffChecker(invocation, o, compatibility)
		options := selection.ProcessOptions("ruff", o.Command, o.Process, cfg, c.WorkingDir)
		if err != nil && options != nil && options.Optional && selection.Module("ruff", o.Command, o.Process, cfg) == nil {
			return nil, nil
		}
		return checker, err
	}}, nil
}

func newRuffChecker(c tools.Context, o Options, compatibility *Compatibility) (*Checker, error) {
	if o.Backend != nil {
		return NewBackend(c.Processes.Ctx, o.Backend, ruffExpressionEnd), nil
	}
	if engine, err := selection.Load("ruff", o.Command, o.Process, c.Config, engineLoader(c)); engine != nil || err != nil {
		if err != nil {
			return nil, fmt.Errorf("could not initialize Ruff: %w", err)
		}
		return NewBackend(c.Processes.Ctx, engine, ruffExpressionEnd), nil
	}
	if !processconfig.Enabled(o.Command, o.Process) {
		return nil, nil
	}
	if err := selection.RequireProcessOptIn("ruff", o.Command, o.Process, c.Config, c.AllowConfigProcesses); err != nil {
		return nil, err
	}
	o.Process = selection.ProcessOptions("ruff", o.Command, o.Process, c.Config, c.WorkingDir)
	cmd, err := c.Processes.ConfiguredCommandRunner(o.Command, o.Process, false)
	if err != nil {
		return nil, fmt.Errorf("could not initialize Ruff: %w", err)
	}
	cmd.MaxExitCode = 1
	cmd.UnsetEnv = UnsetEnvironment()
	backend, prefix := NewProcessBackend(func(ctx context.Context, args []string, source string) ([]byte, error) {
		return cmd.Execute(ctx, args, source)
	}, cmd.Exe, cmd.Args, cmd.Dir)
	cmd.Args = prefix
	if o.Process != nil && o.Process.Optional {
		if compatibility == nil {
			compatibility = &Compatibility{}
		}
		warning := func(err error) {
			if c.Warn != nil {
				c.Warn(fmt.Errorf("skipping automatically discovered Ruff: %w", err))
			}
		}
		backend.OptionalVersion(compatibility, warning)
	}
	checker := NewBackend(c.Processes.Ctx, backend, ruffExpressionEnd)
	return checker, nil
}

func ruffExpressionEnd(src string) (int, bool) {
	lexer := exprlang.NewLexer(src)
	_, err := exprlang.NewParser().Parse(lexer)
	return lexer.Offset(), err == nil
}

func (r *ruffAnalyzer) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	scripts := batch.Scripts
	// Each callback writes only its script's slot. Wait before reading any slots.
	results := make([][]tools.Finding, len(scripts))
	var failure error
	checkers := map[*config.Config]*Checker{}
	pending := []*Checker{r.checker}
	for i, script := range scripts {
		if err := ctx.Err(); err != nil {
			failure = errors.Join(failure, err)
			break
		}
		if script.Input != nil {
			continue
		}
		if config.DiagnosticLevel(script.Config, "ruff") == "off" {
			continue
		}
		settings := Config{}
		if script.Config != nil {
			settings = script.Config.Tools.Ruff
		}
		if settings.Enabled != nil && !*settings.Enabled {
			continue
		}
		checker := r.checker
		if r.forConfig != nil {
			var ok bool
			checker, ok = checkers[script.Config]
			if !ok {
				var err error
				checker, err = r.forConfig(script.Config)
				if err != nil {
					failure = errors.Join(failure, err)
					continue
				}
				checkers[script.Config] = checker
				if checker != nil && checker != r.checker {
					pending = append(pending, checker)
				}
			}
		}
		if checker == nil {
			continue
		}
		err := checker.Check(script.Text, script.Shell, script.Key.String(), settings, func(d Diagnostic) {
			pos, mapped := script.Source.Position(d.Location.Row, d.Location.Column)
			end, endMapped := script.Source.EndPos(d.EndLocation.Row, d.EndLocation.Column)
			if !mapped || !endMapped {
				pos, end = script.Key, nil
			}
			finding := tools.Finding{Rule: "ruff", Message: fmt.Sprintf("Ruff reported issue in this script: %s: %s", d.Code, d.Message), Start: diagnostic.Position{Line: pos.Line, Column: pos.Col}, Code: d.Code, Severity: d.Severity, DocumentationURL: d.URL, Script: i, Content: script.Content}
			if end != nil {
				finding.End = diagnostic.Position{Line: end.Line, Column: end.Col}
			}

			if finding.Severity == "" {
				finding.Severity = "error"
			}
			finding.Path = script.Path
			results[i] = append(results[i], finding)
		})
		if err != nil {
			failure = errors.Join(failure, err)
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
