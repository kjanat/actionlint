package actionlint

import "actionlint.kjanat.dev/internal/ruff"

// ruffRule is the AST/source-map adapter; Python analysis lives in internal/ruff.
type ruffRule struct {
	RuleBase
	checker *ruff.Checker
}

func newRuffRule(c ruleContext) (*ruffRule, error) {
	cmd, err := c.process.configuredCommandRunner(c.ruff, c.ruffOptions, false)
	if err != nil {
		return nil, err
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
	return &ruffRule{RuleBase: builtinRuleBase("ruff"), checker: checker}, nil
}

func ruffExpressionEnd(src string) (int, bool) {
	lexer := NewExprLexer(src)
	_, err := NewExprParser().Parse(lexer)
	return lexer.Offset(), err == nil
}

func ruffShellValue(shell *String) *string {
	if shell == nil {
		return nil
	}
	return &shell.Value
}

func ruffDefaultShell(defaults *Defaults) *string {
	if defaults != nil && defaults.Run != nil {
		return ruffShellValue(defaults.Run.Shell)
	}
	return nil
}

func (r *ruffRule) VisitWorkflowPre(w *Workflow) error {
	r.checker.WorkflowShell(ruffDefaultShell(w.Defaults))
	return nil
}

func (r *ruffRule) VisitJobPre(j *Job) error {
	r.checker.JobShell(ruffDefaultShell(j.Defaults))
	return nil
}

func (r *ruffRule) VisitJobPost(*Job) error           { r.checker.JobShell(nil); return nil }
func (r *ruffRule) VisitWorkflowPost(*Workflow) error { return r.checker.Wait() }

func (r *ruffRule) VisitStep(step *Step) error {
	if r.config.diagnosticLevel("ruff") == "off" {
		return nil
	}
	run, ok := step.Exec.(*ExecRun)
	if !ok || run.Run == nil {
		return nil
	}
	config := RuffToolConfig{}
	if r.config != nil {
		config = r.config.Tools.Ruff
	}
	return r.checker.Check(run.Run.Value, ruffShellValue(run.Shell), run.RunPos.String(), config, func(d ruff.Diagnostic) {
		pos, mapped := run.source.pos(d.Location.Row, d.Location.Column)
		if !mapped {
			pos = run.RunPos
		}
		end, _ := run.source.endPos(d.EndLocation.Row, d.EndLocation.Column)
		if !mapped {
			end = nil
		}
		r.errorfRange(pos, end, "Ruff reported issue in this script: %s: %s", d.Code, d.Message)
		r.errs[len(r.errs)-1].code = d.Code
		r.errs[len(r.errs)-1].severity = "error"
	})
}
