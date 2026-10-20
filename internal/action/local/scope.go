package local

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
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
// Call the returned function when the composite finishes, including on an error.
func (c *Resolver) EnterComposite(call *workflowlang.Step) func() {
	checkout := c.CheckoutState()
	checkoutEnv := c.checkoutEnvUnknown
	persistentEnv := c.persistentEnvUnknown
	shellEnv, checkoutShell := c.shellEnvUnknown, c.checkoutShell
	c.checkoutEnvUnknown = checkoutEnv || execution.CheckoutEnvironmentUnknown(call.Env, c.platform)
	c.shellEnvUnknown = shellEnv || c.shellEnvironmentUnknown(call.Env)
	c.checkoutShell = execution.Shell{}
	return func() {
		c.checkoutEnvUnknown = checkoutEnv
		c.shellEnvUnknown, c.checkoutShell = shellEnv, checkoutShell
		enabled, known := execution.Condition(call.If)
		if known && !enabled {
			c.persistentEnvUnknown = persistentEnv
			c.restoreCheckout(checkout)
		} else if (!known || execution.BoolMayBeTrue(call.ContinueOnError) || execution.BoolMayBeTrue(call.Background)) && checkout != c.CheckoutState() {
			mayFail := execution.BoolMayBeTrue(call.ContinueOnError) || execution.BoolMayBeTrue(call.Background)
			c.restoreCheckout(execution.MergeCheckoutBranches(checkout, c.CheckoutState(), mayFail))
		}
	}
}
