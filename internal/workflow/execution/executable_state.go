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
	placement := CheckoutPlacement{Directory: Directory{Path: checkout}, CaseInsensitive: caseInsensitive}
	for name := range state.changed {
		if _, replaced := placement.Relative(name); replaced {
			delete(state.changed, name)
		}
	}
}

// ForkComposite creates isolated execution state for an action entry or call and its resolved paths.
func (rule *ExecutableChecker) ForkComposite(context invocation.Context, actionPath string, paths Paths) *ExecutableChecker {
	scoped := NewExecutableChecker(rule.context)
	scoped.executableState = rule.executableState
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
	// Conditional calls cannot mutate their caller's maps until branches join.
	if effects.Condition != invocation.ConditionEnabled {
		scoped.changed = maps.Clone(rule.changed)
		scoped.actionChanged = maps.Clone(rule.actionChanged)
	}
	scoped.jobEnv = rule.jobEnv || effects.ShellEnvironmentUnknown
	scoped.jobGitEnv = rule.jobGitEnv || effects.CheckoutEnvironmentUnknown
	scoped.jobPathUnknown = rule.jobPathUnknown || effects.ShellPathUnknown
	if effects.Condition == invocation.ConditionSkipped || effects.Background {
		scoped.afterConcurrentExecution()
	}
	return scoped
}

// JoinComposite merges conditional mode changes of a call back into the caller.
func (rule *ExecutableChecker) JoinComposite(context invocation.Context, child *ExecutableChecker) {
	effects, isCall := context.Facts()
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
	rule.paths, rule.changed, rule.actionChanged = child.paths, child.changed, child.actionChanged
}
