package execution

import (
	"maps"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// executableState tracks when indexed modes still describe execution. Workspace
// and independently downloaded action copies retain separate mutation histories.
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

// ForkComposite creates isolated execution state for a composite call and its resolved paths.
func (rule *ExecutableChecker) ForkComposite(call *workflowlang.Step, actionPath string, paths Paths) *ExecutableChecker {
	scoped := NewExecutableChecker(rule.context)
	scoped.executableState = rule.executableState
	scoped.unix, scoped.caseInsensitive, scoped.shIsDash = rule.unix, rule.caseInsensitive, rule.shIsDash
	if CanRunAfterFailure(call.If) {
		scoped.afterPossibleFailure()
	}
	scoped.paths = paths
	ActionOrigin(&scoped.paths, call, actionPath)
	enabled, conditionKnown := Condition(call.If)
	scoped.skipFindings = rule.skipFindings || conditionKnown && !enabled
	// Conditional calls cannot mutate their caller's maps until branches join.
	if !conditionKnown || !enabled {
		scoped.changed = maps.Clone(rule.changed)
		scoped.actionChanged = maps.Clone(rule.actionChanged)
	}
	scoped.jobEnv = rule.jobEnv || ShellEnvironmentUnknown(call.Env)
	scoped.jobGitEnv = rule.jobGitEnv || CheckoutEnvironmentUnknown(call.Env, scoped.paths.Platform)
	scoped.jobPathUnknown = rule.jobPathUnknown || ShellPathUnknown(call.Env)
	if conditionKnown && !enabled || BoolMayBeTrue(call.Background) {
		scoped.afterConcurrentExecution()
	}
	return scoped
}

// JoinComposite merges conditional mode changes back into the caller.
func (rule *ExecutableChecker) JoinComposite(call *workflowlang.Step, child *ExecutableChecker) {
	enabled, conditionKnown := Condition(call.If)
	if conditionKnown && !enabled {
		return
	}
	if !conditionKnown {
		// Either branch can retain changed modes; a child checkout only resets
		// them when that branch actually runs.
		maps.Copy(rule.changed, child.changed)
		maps.Copy(rule.actionChanged, child.actionChanged)
	}
	rule.actionPristine = rule.actionPristine && child.actionPristine
	rule.repositoryUnknown = child.repositoryUnknown
	if BoolMayBeTrue(call.ContinueOnError) {
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
