package execution

import (
	"errors"

	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

// ErrInvalidActionCall reports a call step that cannot enter an action.
var ErrInvalidActionCall = errors.New("execution: invalid action call")

// CallFacts projects caller effects from the owned syntax view.
func CallFacts(view workflowdecl.StepView, platform runnerlang.Platform) (invocation.Facts, error) {
	if !view.Present() {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	action, ok := view.Action()
	if !ok || !action.Present() {
		return invocation.Facts{}, ErrInvalidActionCall
	}
	position := invocation.Position{}
	if pos, present := view.Pos(); present {
		position = invocation.Position{Line: pos.Line, Column: pos.Col}
	}
	uses := action.Uses()
	effects := invocation.Facts{
		HasUses:                    uses.Present(),
		Step:                       position,
		Condition:                  invocation.ConditionRuntimeDependent,
		AfterFailure:               CanRunAfterFailure(view.Condition()),
		ContinueOnError:            BoolMayBeTrue(view.ContinueOnError().Semantic()),
		Background:                 BoolMayBeTrue(view.Background().Semantic()),
		ShellEnvironmentUnknown:    ShellEnvironmentUnknown(view.Env().Mapping()),
		ShellPathUnknown:           ShellPathUnknown(view.Env().Mapping()),
		CheckoutEnvironmentUnknown: CheckoutEnvironmentUnknown(view.Env().Mapping(), platform),
		LookupEnvironmentUnknown:   ShellEnvironmentUnknownForPlatform(view.Env().Mapping(), platform) || ShellPathUnknownForPlatform(view.Env().Mapping(), platform),
	}
	if uses.Present() {
		effects.Uses = uses.Value()
	}
	if enabled, known := Condition(view.Condition()); known {
		effects.Condition = invocation.ConditionSkipped
		if enabled {
			effects.Condition = invocation.ConditionEnabled
		}
	}
	return effects, nil
}
