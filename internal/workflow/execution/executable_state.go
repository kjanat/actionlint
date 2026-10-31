package execution

import (
	"maps"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

// executableState tracks mutations that invalidate indexed executable modes.
type executableState struct {
	sequential, pristine, actionPristine bool
	repositoryUnknown                    bool
	changed, actionChanged               map[string]bool
}

func newExecutableState(hosted, callerRepositoryUnknown bool) executableState {
	return executableState{
		sequential: true, actionPristine: hosted,
		repositoryUnknown: callerRepositoryUnknown || !hosted,
		changed:           make(map[string]bool), actionChanged: make(map[string]bool),
	}
}

// fork copies the state for a composite entry or call. The copy gets its own
// changed and actionChanged maps, so the child's mode observations reach the
// caller only through JoinComposite.
func (state *executableState) fork() executableState {
	forked := *state
	forked.changed, forked.actionChanged = maps.Clone(state.changed), maps.Clone(state.actionChanged)
	return forked
}

func (state *executableState) afterPossibleFailure() {
	state.pristine, state.repositoryUnknown = false, true
	state.actionPristine = false
}

func (state *executableState) afterConcurrentExecution() {
	state.sequential, state.pristine = false, false
	state.actionPristine = false
}

func (state *executableState) afterKnownCheckout(checkout string, caseInsensitive bool) {
	state.pristine, state.repositoryUnknown = true, false
	// Reset only paths replaced by this checkout; sibling and independent
	// action copies keep their own mutation histories.
	placement := NewCheckoutPlacement(Checkout{Directory: Directory{Path: checkout}, CaseInsensitive: caseInsensitive}, CheckoutPlacement{})
	for name := range state.changed {
		if _, replaced := placement.Relative(name); replaced {
			delete(state.changed, name)
		}
	}
}

// ForkComposite creates isolated execution state for an action entry or call and its resolved paths.
// The fork shares the caller's ExecutableContext, so its findings go to the same Report.
func (rule *ExecutableChecker) ForkComposite(context invocation.Context, actionPath string, paths Paths) *ExecutableChecker {
	scoped := NewExecutableChecker(rule.context)
	scoped.executableState = rule.fork()
	scoped.unix, scoped.caseInsensitive, scoped.shIsDash = rule.unix, rule.caseInsensitive, rule.shIsDash
	effects, isCall := context.Facts()
	if isCall && effects.AfterFailure {
		scoped.afterPossibleFailure()
	}
	scoped.paths = paths
	ActionOrigin(&scoped.paths, context, actionPath)
	scoped.skipFindings = rule.skipFindings
	scoped.jobEnv, scoped.jobGitEnv, scoped.jobPathUnknown = rule.jobEnv, rule.jobGitEnv, rule.jobPathUnknown
	if !isCall {
		return scoped
	}
	scoped.skipFindings = rule.skipFindings || effects.Condition == invocation.ConditionSkipped
	scoped.jobEnv = rule.jobEnv || effects.ShellEnvironmentUnknown
	scoped.jobGitEnv = rule.jobGitEnv || effects.CheckoutEnvironmentUnknown
	scoped.jobPathUnknown = rule.jobPathUnknown || effects.ShellPathUnknown
	if effects.Condition == invocation.ConditionSkipped || effects.Background {
		scoped.afterConcurrentExecution()
	}
	return scoped
}

// JoinComposite merges the mode changes of an entry or call back into the caller.
func (rule *ExecutableChecker) JoinComposite(context invocation.Context, child *ExecutableChecker) {
	effects, isCall := context.Facts()
	if !isCall || effects.Condition == invocation.ConditionEnabled {
		// An entry or unconditional call ran in the caller's place, so its mode
		// observations replace the caller's even when it may have failed.
		rule.changed, rule.actionChanged = child.changed, child.actionChanged
	}
	if !isCall || effects.Condition == invocation.ConditionSkipped {
		return
	}
	conditionKnown := effects.Condition == invocation.ConditionEnabled
	if !conditionKnown {
		// A conditional checkout cannot prove that changed modes were reset.
		maps.Copy(rule.changed, child.changed)
		maps.Copy(rule.actionChanged, child.actionChanged)
	}
	rule.actionPristine = rule.actionPristine && child.actionPristine
	rule.repositoryUnknown = child.repositoryUnknown
	if effects.ContinueOnError {
		// The caller may continue after a failed child checkout.
		rule.pristine, rule.repositoryUnknown = false, true
		rule.sequential = child.sequential
		return
	}
	if !conditionKnown {
		// A conditional checkout cannot establish unconditional caller state.
		rule.pristine = false
		rule.sequential = child.sequential
		return
	}
	rule.pristine, rule.sequential = child.pristine, child.sequential
	child.paths.ActionPath = rule.paths.ActionPath
	child.paths.ActionRunnerPath = rule.paths.ActionRunnerPath
	child.paths.ActionIndependent = rule.paths.ActionIndependent
	rule.paths = child.paths
}
