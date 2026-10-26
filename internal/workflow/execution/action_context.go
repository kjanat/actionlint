package execution

import (
	"errors"

	"actionlint.kjanat.dev/v2/diagnostic"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

// ErrInvalidActionCall reports a call step that cannot enter an action.
var ErrInvalidActionCall = errors.New("execution: invalid action call")

// CallEffects holds caller-side facts of one action call, computed once.
type CallEffects struct {
	Step                       diagnostic.Position
	Uses                       string
	Condition                  invocation.Condition
	AfterFailure               bool
	ContinueOnError            bool
	Background                 bool
	ShellEnvironmentUnknown    bool
	ShellPathUnknown           bool
	CheckoutEnvironmentUnknown bool
	LookupEnvironmentUnknown   bool
}

// ActionContext is an immutable action entry or call. The zero value is an entry.
type ActionContext struct {
	effects *CallEffects
	hasUses bool
}

// ActionEntry returns the context of an analysis entry, which has no incoming call.
func ActionEntry() ActionContext { return ActionContext{} }

// ActionCall precomputes the effects of an action call step on its caller.
// An unresolved call may have nil Uses, which is retained as empty.
func ActionCall(call *workflowlang.Step, platform runnerlang.Platform) (ActionContext, error) {
	if call == nil {
		return ActionContext{}, ErrInvalidActionCall
	}
	action, ok := call.Exec.(*workflowlang.ExecAction)
	if !ok || action == nil {
		return ActionContext{}, ErrInvalidActionCall
	}
	effects := CallEffects{
		Step:                       workflowfacts.Position(call.Pos),
		Condition:                  invocation.ConditionRuntimeDependent,
		AfterFailure:               CanRunAfterFailure(call.If),
		ContinueOnError:            BoolMayBeTrue(call.ContinueOnError),
		Background:                 BoolMayBeTrue(call.Background),
		ShellEnvironmentUnknown:    ShellEnvironmentUnknown(call.Env),
		ShellPathUnknown:           ShellPathUnknown(call.Env),
		CheckoutEnvironmentUnknown: CheckoutEnvironmentUnknown(call.Env, platform),
		LookupEnvironmentUnknown:   ShellEnvironmentUnknownForPlatform(call.Env, platform) || ShellPathUnknownForPlatform(call.Env, platform),
	}
	if action.Uses != nil {
		effects.Uses = action.Uses.Value
	}
	if enabled, known := Condition(call.If); known {
		effects.Condition = invocation.ConditionSkipped
		if enabled {
			effects.Condition = invocation.ConditionEnabled
		}
	}
	return ActionContext{effects: &effects, hasUses: action.Uses != nil}, nil
}

// Effects returns a copy of the call effects, or false for an entry.
func (c ActionContext) Effects() (CallEffects, bool) {
	if c.effects == nil {
		return CallEffects{}, false
	}
	return *c.effects, true
}
