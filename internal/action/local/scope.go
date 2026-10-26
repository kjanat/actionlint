package local

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

// BeginJob resets checkout and persistent environment assumptions for a new job.
func (c *Resolver) BeginJob(workflowEnv *workflowlang.Env, workflowShell execution.ShellValue, job *workflowlang.Job) {
	c.setCheckout(execution.Directory{})
	knowledge := c.Runners.Job(job)
	c.platform = knowledge.Platform
	c.caseInsensitive = knowledge.CaseInsensitive
	c.checkoutEnvUnknown = !knowledge.PathsKnown || execution.CheckoutEnvironmentUnknown(workflowEnv, c.platform) || execution.CheckoutEnvironmentUnknown(job.Env, c.platform)
	c.persistentEnvUnknown = false
	c.shellEnvUnknown = c.shellEnvironmentUnknown(workflowEnv) || c.shellEnvironmentUnknown(job.Env)
	runnerShell := execution.ShellValue{Kind: execution.ShellUnknown}
	if knowledge.Shell != "" && (c.Runners != nil || c.platform != runnerlang.Any) {
		runnerShell = execution.ShellValueOf(&workflowlang.String{Value: knowledge.Shell})
	}
	if container := execution.ContainerShell(job.Container); container.Kind != execution.ShellUnspecified {
		runnerShell = container
	}
	c.checkoutShell = execution.ResolveShell(&workflowlang.ExecRun{}, execution.DefaultShell(job.Defaults), workflowShell, runnerShell)
}

// EnterComposite saves invocation-local assumptions and returns their branch join.
// An action entry has no caller to join with. Call the returned function when
// the composite finishes, including on an error.
func (c *Resolver) EnterComposite(context invocation.Context) func() {
	effects, isCall := context.Facts()
	checkout := c.CheckoutState()
	checkoutEnv := c.checkoutEnvUnknown
	persistentEnv := c.persistentEnvUnknown
	shellEnv, checkoutShell := c.shellEnvUnknown, c.checkoutShell
	if isCall {
		c.checkoutEnvUnknown = checkoutEnv || effects.CheckoutEnvironmentUnknown
		c.shellEnvUnknown = shellEnv || effects.LookupEnvironmentUnknown
	}
	c.checkoutShell = execution.Shell{}
	return func() {
		c.checkoutEnvUnknown = checkoutEnv
		c.shellEnvUnknown, c.checkoutShell = shellEnv, checkoutShell
		if !isCall {
			return
		}
		if effects.Condition == invocation.ConditionSkipped {
			c.persistentEnvUnknown = persistentEnv
			c.restoreCheckout(checkout)
			return
		}
		mayFail := effects.ContinueOnError || effects.Background
		if (effects.Condition == invocation.ConditionRuntimeDependent || mayFail) && checkout != c.CheckoutState() {
			c.restoreCheckout(execution.MergeCheckoutBranches(checkout, c.CheckoutState(), mayFail))
		}
	}
}
