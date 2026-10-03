package actionlint

import (
	"context"
	"errors"
)

// compositeAnalyzer owns script-analyzer policy and invocation-specific state,
// leaving Visitor responsible for the workflow's ordinary pass lifecycle.
type compositeAnalyzer struct {
	ctx            context.Context
	actions        *LocalActionsCache
	passes         []Pass
	rules          []compositeScriptRules
	workflowStart  int
	actionPathErr  error
	workflowGitEnv bool
}

func (analysis *compositeAnalyzer) cancelled() error {
	if analysis.ctx == nil {
		return nil
	}
	return analysis.ctx.Err()
}

func (analysis *compositeAnalyzer) beginWorkflow(workflow *Workflow) {
	analysis.workflowStart = len(analysis.rules)
	analysis.actionPathErr = nil
	analysis.workflowGitEnv = checkoutEnvironmentUnknown(workflow.Env)
}

func (analysis *compositeAnalyzer) deferWorkflowError(pass Pass, err error) bool {
	if _, shellcheck := pass.(*RuleShellcheck); shellcheck && errors.Is(err, errConfigActionPathUnavailable) {
		// Validate action-only configuration when entering the actual composite.
		analysis.actionPathErr = err
		return true
	}
	return false
}

func (analysis *compositeAnalyzer) validateWorkflow() error {
	if analysis.actionPathErr == nil {
		return nil
	}
	for _, composite := range analysis.rules[analysis.workflowStart:] {
		for _, rule := range composite.rules {
			if _, shellcheck := rule.(*RuleShellcheck); shellcheck {
				return nil
			}
		}
	}
	return analysis.actionPathErr
}

func (analysis *compositeAnalyzer) beginJob(job *Job) {
	analysis.actions.setCheckout(runDirectory{})
	analysis.actions.platform = runnerPlatform(job.RunsOn)
	analysis.actions.caseInsensitive = analysis.actions.platform == platformKindWindows || macOSRunner(job.RunsOn)
	analysis.actions.checkoutEnvUnknown = analysis.workflowGitEnv || checkoutEnvironmentUnknown(job.Env)
}

func (analysis *compositeAnalyzer) invalidateCheckout() {
	analysis.actions.setCheckout(runDirectory{kind: directoryUnknown})
}

func (analysis *compositeAnalyzer) visitStep(step *Step) error {
	if err := analysis.cancelled(); err != nil {
		return err
	}
	analysis.actions.observeCheckout(step)
	for _, pass := range analysis.passes {
		if shellcheck, ok := pass.(*RuleShellcheck); ok {
			compositeCheckoutPaths(shellcheck, analysis.actions)
		}
		if _, script := pass.(*RuleExecutableBit); script {
			if _, action := step.Exec.(*ExecAction); action {
				continue // Preserve checkout state while entering a local composite.
			}
		}
		if err := pass.VisitStep(step); err != nil {
			return err
		}
	}
	if _, action := step.Exec.(*ExecAction); !action {
		return nil
	}
	var scripts []Rule
	for _, pass := range analysis.passes {
		switch rule := pass.(type) {
		case *RuleShellcheck:
			scripts = append(scripts, rule)
		case *RulePyflakes:
			scripts = append(scripts, rule)
		case *RuleExecutableBit:
			scripts = append(scripts, rule)
		}
	}
	return analysis.visitActionScripts(step, scripts, make(map[string]bool))
}
