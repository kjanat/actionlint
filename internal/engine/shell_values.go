package engine

import (
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/schema"
)

type shellValueKind uint8

const (
	shellValueUnspecified shellValueKind = iota
	shellValueSource
	shellValueEvaluated
	shellValueUnknown
)

// shellValue distinguishes an absent shell from an unresolved override.
type shellValue struct {
	kind  shellValueKind
	value *workflowlang.String
}

func shellValueFromString(value *workflowlang.String) shellValue {
	if value == nil {
		return shellValue{}
	}
	return shellValue{kind: shellValueSource, value: value}
}

func defaultsShellValue(defaults *workflowlang.Defaults) shellValue {
	if defaults == nil || defaults.Run == nil {
		return shellValue{}
	}
	run := defaults.Run
	if run.Expression == nil {
		return shellValueFromString(run.Shell)
	}
	value, known := workflowlang.WorkflowExpressionLiteral(run.Expression)
	if !known || len(schema.LiteralErrors(schema.DefaultsRun, value, "defaults.run")) != 0 {
		return shellValue{kind: shellValueUnknown}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return shellValue{kind: shellValueUnknown}
	}
	shell := schema.Property(object, "shell")
	if shell == nil {
		return shellValue{}
	}
	name, ok := workflowlang.WorkflowScalarString(shell)
	if !ok {
		return shellValue{kind: shellValueUnknown}
	}
	// Evaluated strings are data, including any expression delimiters they contain.
	return shellValue{kind: shellValueEvaluated, value: &workflowlang.String{Value: name, Pos: run.Expression.Pos}}
}

func runnerPlatformLabels(runner *workflowlang.Runner) []*workflowlang.String {
	if runner == nil {
		return nil
	}
	var labels []*workflowlang.String
	for _, expression := range []*workflowlang.String{runner.Expression, runner.LabelsExpr} {
		if expression != nil {
			if known, ok := knownRunnerExpressionLabels(expression); ok {
				labels = append(labels, known...)
			}
		}
	}
	for _, label := range runner.Labels {
		if known, ok := knownRunnerExpressionLabels(label); ok {
			labels = append(labels, known...)
		} else {
			labels = append(labels, label)
		}
	}
	return labels
}
