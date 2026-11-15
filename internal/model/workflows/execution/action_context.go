package execution

import (
	"errors"

	workflowconditions "actionlint.kjanat.dev/v2/internal/model/workflows/conditions"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// ErrInvalidActionCall reports a call step that cannot enter an action.
var ErrInvalidActionCall = errors.New("execution: invalid action call")

// CallFacts projects caller effects from the owned syntax view.
func CallFacts(view workflowdecl.Step, platform runnerlang.Platform) (invocation.Facts, error) {
	if !view.Present() {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	action, ok := view.Action()
	if !ok {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	position := invocation.Position{}
	if pos, present := view.Position(); present {
		position = invocation.Position{Line: pos.Line, Column: pos.Column}
	}
	env := workflowdecl.Environment(view.Env)
	effects := invocation.Facts{
		HasUses:                    action.Uses != nil,
		Step:                       position,
		Condition:                  invocation.ConditionRuntimeDependent,
		AfterFailure:               workflowconditions.CanRunAfterFailure(view.Condition()),
		ContinueOnError:            workflowconditions.BoolMayBeTrue(workflowdecl.Bool(view.ContinueOnError)),
		Background:                 workflowconditions.BoolMayBeTrue(workflowdecl.Bool(view.Background)),
		ShellEnvironmentUnknown:    ShellEnvironmentUnknown(env),
		ShellPathUnknown:           ShellPathUnknown(env),
		CheckoutEnvironmentUnknown: CheckoutEnvironmentUnknown(env, platform),
		LookupEnvironmentUnknown:   ShellEnvironmentUnknownForPlatform(env, platform) || ShellPathUnknownForPlatform(env, platform),
	}
	effects.Uses = action.Uses.Text()
	if enabled, known := workflowconditions.Condition(view.Condition()); known {
		effects.Condition = invocation.ConditionSkipped
		if enabled {
			effects.Condition = invocation.ConditionEnabled
		}
	}
	return effects, nil
}
