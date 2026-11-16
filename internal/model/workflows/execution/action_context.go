package execution

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowconditions "actionlint.kjanat.dev/v2/internal/model/workflows/conditions"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// ErrInvalidActionCall reports a call step that cannot enter an action.
var ErrInvalidActionCall = errors.New("execution: invalid action call")

// CallFacts projects caller effects from the owned syntax view.
func CallFacts(view workflows.Step, platform runnerlang.Platform) (invocation.Facts, error) {
	if !view.Present() {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	action, ok := view.Action()
	if !ok {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	position := source.Position{}
	if pos, present := view.Position(); present {
		position = pos
	}
	env := workflows.Environment(view.Env)
	effects := invocation.Facts{
		HasUses:                    action.Uses != nil,
		Step:                       position,
		Condition:                  invocation.ConditionRuntimeDependent,
		AfterFailure:               workflowconditions.CanRunAfterFailure(view.Condition()),
		ContinueOnError:            workflowconditions.BoolMayBeTrue(workflows.Bool(view.ContinueOnError)),
		Background:                 workflowconditions.BoolMayBeTrue(workflows.Bool(view.Background)),
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
