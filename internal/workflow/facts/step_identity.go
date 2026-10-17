package facts

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// stepFingerprint includes the step's structural values, without source positions,
// quoting, or parser source maps. Length-prefixed fields preserve boundaries;
// sorted mapping keys make presentation-only changes irrelevant to identity.
func stepFingerprint(step *workflowlang.Step) string {
	var b strings.Builder
	field := func(value string) {
		b.WriteString(strconv.Itoa(len(value)))
		b.WriteByte(':')
		b.WriteString(value)
	}
	text := func(value *workflowlang.String) {
		if value == nil {
			field("missing")
		} else {
			field("string")
			field(value.Value)
		}
	}
	boolean := func(value *workflowlang.Bool) {
		if value == nil {
			field("missing")
		} else {
			field(strconv.FormatBool(value.Value))
			text(value.Expression)
		}
	}
	var env func(*workflowlang.Env)
	env = func(value *workflowlang.Env) {
		if value == nil {
			field("missing")
			return
		}
		text(value.Expression)
		field(strconv.Itoa(len(value.Vars)))
		for _, key := range slices.Sorted(maps.Keys(value.Vars)) {
			field(key)
			text(value.Vars[key].Name)
			text(value.Vars[key].Value)
		}
		field(strconv.Itoa(len(value.Insertions)))
		for _, insertion := range value.Insertions {
			env(insertion)
		}
	}
	var inputs func(*workflowlang.ActionInputMapping)
	inputs = func(value *workflowlang.ActionInputMapping) {
		text(value.Expression)
		field(strconv.Itoa(len(value.Inputs)))
		for _, key := range slices.Sorted(maps.Keys(value.Inputs)) {
			field(key)
			text(value.Inputs[key].Name)
			text(value.Inputs[key].Value)
		}
		field(strconv.Itoa(len(value.Insertions)))
		for _, insertion := range value.Insertions {
			inputs(insertion)
		}
	}
	text(step.ID)
	text(step.Name)
	text(step.If)
	text(step.DeclaredUses)
	env(step.Env)
	boolean(step.ContinueOnError)
	boolean(step.Background)
	if step.TimeoutMinutes == nil {
		field("missing")
	} else {
		field(strconv.FormatFloat(step.TimeoutMinutes.Value, 'g', -1, 64))
		text(step.TimeoutMinutes.Expression)
	}
	if step.Exec == nil {
		field("missing")
		return b.String()
	}
	field(strconv.Itoa(int(step.Exec.Kind())))
	switch exec := step.Exec.(type) {
	case *workflowlang.ExecRun:
		text(exec.Run)
		text(exec.Shell)
		text(exec.WorkingDirectory)
	case *workflowlang.ExecAction:
		text(exec.Uses)
		inputs(&workflowlang.ActionInputMapping{Inputs: exec.Inputs, Expression: exec.InputsExpression, Insertions: exec.InputInsertions})
		text(exec.Entrypoint)
		text(exec.Args)
		field(strconv.FormatBool(exec.EntrypointEvaluated))
		field(strconv.FormatBool(exec.ArgsEvaluated))
		field(strconv.Itoa(len(exec.InputNameExpressions)))
		for _, name := range exec.InputNameExpressions {
			text(name)
		}
	case *workflowlang.ExecWait:
		field(strconv.FormatBool(exec.All))
		field(strconv.Itoa(len(exec.Names)))
		for _, name := range exec.Names {
			text(name)
		}
	case *workflowlang.ExecCancel:
		text(exec.Name)
	case *workflowlang.ExecParallel:
		field(strconv.Itoa(len(exec.Steps)))
		for _, child := range exec.Steps {
			field(stepFingerprint(child))
		}
	}
	return b.String()
}
