package execution

import (
	"errors"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

// ErrInvalidActionCall reports a call step that cannot enter an action.
var ErrInvalidActionCall = errors.New("execution: invalid action call")

// CallFacts computes the effects of an action call step on its caller.
func CallFacts(call *workflowlang.Step, platform runnerlang.Platform) (invocation.Facts, error) {
	if call == nil {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	action, ok := call.Exec.(*workflowlang.ExecAction)
	if !ok || action == nil {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	position := workflowfacts.Position(call.Pos)
	effects := invocation.Facts{
		HasUses:                    action.Uses != nil,
		Step:                       invocation.Position{Line: position.Line, Column: position.Column},
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
	return effects, nil
}
