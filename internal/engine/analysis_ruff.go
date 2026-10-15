package engine

import (
	"actionlint.kjanat.dev/internal/ruff"
	"context"
	"errors"
	"fmt"
	"slices"
)

// ruffAnalyzer consumes prepared scripts. It has no traversal or diagnostic state.
type ruffAnalyzer struct{ checker *ruff.Checker }

func newRuffAnalyzer(c ruleContext) (scriptAnalyzer, error) {
	cmd, err := c.process.configuredCommandRunner(c.ruff, c.ruffOptions, false)
	if err != nil {
		return nil, fmt.Errorf("could not initialize Ruff: %w", err)
	}
	cmd.maxExitCode = 1
	cmd.unsetEnv = ruff.UnsetEnvironment()
	checker := ruff.New(cmd.run, cmd.wait, ruffExpressionEnd)
	cmd.args = checker.CommandArguments(cmd.exe, cmd.args)
	checker.WorkingDirectory(cmd.dir)
	if c.ruffOptions != nil && c.ruffOptions.Optional {
		compatibility := c.ruffCompatibility
		if compatibility == nil {
			compatibility = &ruff.Compatibility{}
		}
		checker.OptionalVersion(compatibility, c.process.ctx, c.ruffWarning)
	}
	return &ruffAnalyzer{checker: checker}, nil
}

func ruffExpressionEnd(src string) (int, bool) {
	lexer := NewExprLexer(src)
	_, err := NewExprParser().Parse(lexer)
	return lexer.Offset(), err == nil
}

func (r *ruffAnalyzer) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*Error, error) {
	scripts := batch.scripts
	// Each callback writes only its script's slot. Wait before reading any slots.
	results := make([][]*Error, len(scripts))
	var failure error
	for i, script := range scripts {
		if err := ctx.Err(); err != nil {
			failure = err
			break
		}
		if script.config.diagnosticLevel("ruff") == "off" {
			continue
		}
		config := RuffToolConfig{}
		if script.config != nil {
			config = script.config.Tools.Ruff
		}
		run := script.run
		err := r.checker.Check(run.Run.Value, script.shell, run.RunPos.String(), config, func(d ruff.Diagnostic) {
			pos, mapped := run.source.pos(d.Location.Row, d.Location.Column)
			end, endMapped := run.source.endPos(d.EndLocation.Row, d.EndLocation.Column)
			if !mapped || !endMapped {
				pos, end = run.RunPos, nil
			}
			finding := errorfAt(pos, "ruff", "Ruff reported issue in this script: %s: %s", d.Code, d.Message)
			finding.endPosition = end
			if end != nil && end.Line == pos.Line && end.Col > pos.Col {
				finding.endColumn = end.Col - 1
			}
			finding.code, finding.severity = d.Code, "error"
			finding.ignoreOrigin = script.origin
			finding.Filepath, finding.source = script.path, script.content
			results[i] = append(results[i], finding)
		})
		if err != nil {
			failure = err
			break
		}
	}
	failure = errors.Join(failure, r.checker.Wait(), ctx.Err())
	return slices.Concat(results...), failure
}
