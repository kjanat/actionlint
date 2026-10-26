package local

import (
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/mapping"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

// BeginJob resets checkout and persistent environment assumptions for a new job.
func (c *Resolver) BeginJob(workflowEnv mapping.Mapping, workflowShell value.Value, job documentparse.JobView) {
	c.setCheckout(execution.Directory{})
	knowledge := c.Runners.Job(job)
	c.platform = knowledge.Platform
	c.caseInsensitive = knowledge.CaseInsensitive
	jobEnv := job.Env().Mapping()
	c.checkoutEnvUnknown = !knowledge.PathsKnown || execution.CheckoutEnvironmentUnknown(workflowEnv, c.platform) || execution.CheckoutEnvironmentUnknown(jobEnv, c.platform)
	c.persistentEnvUnknown = false
	c.shellEnvUnknown = c.shellEnvironmentUnknown(workflowEnv) || c.shellEnvironmentUnknown(jobEnv)
	runnerShell := value.DynamicValue("", value.KindUnknown, value.Origin{})
	if knowledge.Shell != "" && (c.Runners != nil || c.platform != runnerlang.Any) {
		runnerShell = value.YAMLScalar(knowledge.Shell, "!!str", value.Origin{})
	}
	if container := execution.ContainerShell(job.Container().Semantic()); container.State() != value.Missing {
		runnerShell = container
	}
	c.checkoutShell = execution.ResolveShell(value.Value{}, execution.DefaultShell(job.Defaults().Run().Semantic()), workflowShell, runnerShell)
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
