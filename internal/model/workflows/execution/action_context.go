package execution

import (
	"errors"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/conditions"

	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

var ErrInvalidActionCall = errors.New("execution: invalid action call")

func CallFacts(view workflows.BoundStep, platform runners.Platform) (invocation.Facts, error) {
	if !view.Present() {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	action, ok := view.Action()
	if !ok {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	position := sourcetext.Position{}
	if pos, present := view.Position(); present {
		position = pos
	}
	env := workflows.Environment(view.Env)
	effects := invocation.Facts{
		HasUses:                    action.Uses != nil,
		Step:                       position,
		Condition:                  invocation.ConditionRuntimeDependent,
		AfterFailure:               conditions.CanRunAfterFailure(view.Condition()),
		ContinueOnError:            conditions.BoolMayBeTrue(workflows.Bool(view.ContinueOnError)),
		Background:                 conditions.BoolMayBeTrue(workflows.Bool(view.Background)),
		ShellEnvironmentUnknown:    ShellEnvironmentUnknown(env),
		ShellPathUnknown:           ShellPathUnknown(env),
		CheckoutEnvironmentUnknown: CheckoutEnvironmentUnknown(env, platform),
		LookupEnvironmentUnknown:   ShellEnvironmentUnknownForPlatform(env, platform) || ShellPathUnknownForPlatform(env, platform),
		Uses:                       action.Uses.Text(),
	}
	if enabled, known := conditions.Condition(view.Condition()); known {
		effects.Condition = invocation.ConditionSkipped
		if enabled {
			effects.Condition = invocation.ConditionEnabled
		}
	}
	return effects, nil
}
