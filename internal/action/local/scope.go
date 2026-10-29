package local

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

var (
	ErrInvalidCompositeContext = errors.New("local action resolver requires a valid composite context")
	ErrCompositeScopeActive    = errors.New("local action resolver already has an open composite child")
	ErrForeignCompositeScope   = errors.New("local action resolver child belongs to a different parent")
	ErrCompositeScopeJoined    = errors.New("local action resolver composite child is already joined")
	ErrCompositeParentChanged  = errors.New("local action resolver parent changed while its composite child was open")
)

type resolverState struct {
	checkout             execution.CheckoutPlacement
	platform             runnerlang.Platform
	caseInsensitive      bool
	checkoutEnvUnknown   bool
	persistentEnvUnknown bool
	shellEnvUnknown      bool
	checkoutShell        execution.Shell
}

type compositeBranch struct {
	parent  *Resolver
	context invocation.Context
	before  resolverState
}

// state requires c.mu or exclusive traversal ownership.
func (c *Resolver) state() resolverState {
	return resolverState{
		checkout: c.checkout, platform: c.platform, caseInsensitive: c.caseInsensitive,
		checkoutEnvUnknown: c.checkoutEnvUnknown, persistentEnvUnknown: c.persistentEnvUnknown,
		shellEnvUnknown: c.shellEnvUnknown, checkoutShell: c.checkoutShell,
	}
}

// BeginJob resets checkout and persistent environment assumptions for a new job.
func (c *Resolver) BeginJob(workflowEnv mapping.Mapping, workflowShell value.Value, job workflowdecl.JobView) {
	c.setCheckout(execution.Directory{})
	knowledge := c.runners.Job(job)
	c.platform = knowledge.Platform
	c.caseInsensitive = knowledge.CaseInsensitive
	jobEnv := job.Env().Mapping()
	c.checkoutEnvUnknown = !knowledge.PathsKnown || execution.CheckoutEnvironmentUnknown(workflowEnv, c.platform) || execution.CheckoutEnvironmentUnknown(jobEnv, c.platform)
	c.persistentEnvUnknown = false
	c.shellEnvUnknown = c.shellEnvironmentUnknown(workflowEnv) || c.shellEnvironmentUnknown(jobEnv)
	runnerShell := value.DynamicValue("", value.KindUnknown, value.Origin{})
	if knowledge.Shell != "" && (c.runners != nil || c.platform != runnerlang.Any) {
		runnerShell = value.YAMLScalar(knowledge.Shell, "!!str", value.Origin{})
	}
	if container := execution.ContainerShell(job.Container().Semantic()); container.State() != value.Missing {
		runnerShell = container
	}
	c.checkoutShell = execution.ResolveShell(value.Value{}, execution.DefaultShell(job.Defaults().Run().Semantic()), workflowShell, runnerShell)
}

// ForkComposite creates an isolated traversal frame. The caller must verify
// graph ownership of context and join the child even when traversal fails.
// Only one child may be open per parent; a child may fork its own nested child.
func (c *Resolver) ForkComposite(context invocation.Context) (*Resolver, error) {
	if c == nil || !context.Valid() {
		return nil, ErrInvalidCompositeContext
	}
	owner := c.documentOwner()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired {
		return nil, ErrCompositeScopeJoined
	}
	if c.activeChild != nil {
		return nil, ErrCompositeScopeActive
	}
	before := c.state()
	child := &Resolver{
		runners: c.runners, base: c.base, sourceMappings: c.sourceMappings, documents: owner,
		checkout: before.checkout, platform: before.platform, caseInsensitive: before.caseInsensitive,
		checkoutEnvUnknown: before.checkoutEnvUnknown, persistentEnvUnknown: before.persistentEnvUnknown,
		shellEnvUnknown: before.shellEnvUnknown,
		branch:          &compositeBranch{parent: c, context: context, before: before},
	}
	effects, isCall := context.Facts()
	if isCall {
		child.checkoutEnvUnknown = before.checkoutEnvUnknown || effects.CheckoutEnvironmentUnknown
		child.shellEnvUnknown = before.shellEnvUnknown || effects.LookupEnvironmentUnknown
	}
	c.activeChild = child
	return child, nil
}

// JoinComposite applies only the child's checkout and persistent environment
// effects. Caller shell/environment assumptions never move into the parent.
// A stale parent is preserved and reported, rather than silently overwritten.
func (c *Resolver) JoinComposite(child *Resolver) error {
	if c == nil || child == nil || c == child || child.branch == nil || child.branch.parent != c {
		return ErrForeignCompositeScope
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	child.mu.Lock()
	defer child.mu.Unlock()
	if c.retired || child.retired {
		return ErrCompositeScopeJoined
	}
	if c.activeChild != child {
		return ErrForeignCompositeScope
	}
	if child.activeChild != nil {
		return ErrCompositeScopeActive
	}
	child.retired = true
	c.activeChild = nil
	if c.state() != child.branch.before {
		return ErrCompositeParentChanged
	}
	effects, isCall := child.branch.context.Facts()
	if isCall && effects.Condition == invocation.ConditionSkipped {
		return nil
	}
	checkout := child.checkout
	mayFail := effects.ContinueOnError || effects.Background
	if isCall && (effects.Condition == invocation.ConditionRuntimeDependent || mayFail) && child.branch.before.checkout != checkout {
		checkout = execution.MergeCheckoutBranches(child.branch.before.checkout, checkout, mayFail)
	}
	c.checkout, c.persistentEnvUnknown = checkout, child.persistentEnvUnknown
	return nil
}
