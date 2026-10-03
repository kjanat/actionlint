package actionlint

import "maps"

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
	placement := checkoutPlacement{directory: runDirectory{path: checkout}, caseInsensitive: caseInsensitive}
	for name := range state.changed {
		if _, replaced := placement.relative(name); replaced {
			delete(state.changed, name)
		}
	}
}

func (rule *RuleExecutableBit) forkComposite(call *Step, actionPath string, actions *LocalActionsCache) *RuleExecutableBit {
	scoped := newRuleExecutableBit(rule.context)
	scoped.executableState = rule.executableState
	scoped.unix, scoped.caseInsensitive, scoped.shIsDash = rule.unix, rule.caseInsensitive, rule.shIsDash
	if stepCanRunAfterFailure(call.If) {
		scoped.afterPossibleFailure()
	}
	scoped.paths = rule.paths
	compositeCheckoutPaths(scoped, actions)
	compositeActionOrigin(&scoped.paths, call, actionPath)
	enabled, conditionKnown := invocationCondition(call.If)
	scoped.skipFindings = rule.skipFindings || conditionKnown && !enabled
	// Conditional calls cannot mutate their caller's maps until branches join.
	if !conditionKnown || !enabled {
		scoped.changed = maps.Clone(rule.changed)
		scoped.actionChanged = maps.Clone(rule.actionChanged)
	}
	scoped.jobEnv = rule.jobEnv || shellEnvironmentUnknown(call.Env)
	scoped.jobGitEnv = rule.jobGitEnv || checkoutEnvironmentUnknown(call.Env)
	scoped.jobPathUnknown = rule.jobPathUnknown || shellPathUnknown(call.Env)
	if conditionKnown && !enabled || boolMayBeTrue(call.Background) {
		scoped.afterConcurrentExecution()
	}
	return scoped
}

func (rule *RuleExecutableBit) joinComposite(call *Step, child *RuleExecutableBit) {
	enabled, conditionKnown := invocationCondition(call.If)
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
	if boolMayBeTrue(call.ContinueOnError) {
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
	child.paths.actionPath = rule.paths.actionPath
	child.paths.actionRunnerPath = rule.paths.actionRunnerPath
	child.paths.actionIndependent = rule.paths.actionIndependent
	rule.paths, rule.changed, rule.actionChanged = child.paths, child.changed, child.actionChanged
}
