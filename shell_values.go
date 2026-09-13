package actionlint

import "strconv"

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
	value *String
}

func shellValueFromString(value *String) shellValue {
	if value == nil {
		return shellValue{}
	}
	return shellValue{kind: shellValueSource, value: value}
}

func defaultsShellValue(defaults *Defaults) shellValue {
	if defaults == nil || defaults.Run == nil {
		return shellValue{}
	}
	run := defaults.Run
	if run.Expression == nil {
		return shellValueFromString(run.Shell)
	}
	value, known := workflowExpressionLiteral(run.Expression)
	if !known || len(workflowExpressionLiteralErrors(workflowDefaultsRun, value, "defaults.run")) != 0 {
		return shellValue{kind: shellValueUnknown}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return shellValue{kind: shellValueUnknown}
	}
	shell := workflowObjectProperty(object, "shell")
	if shell == nil {
		return shellValue{}
	}
	name, ok := workflowScalarString(shell)
	if !ok {
		return shellValue{kind: shellValueUnknown}
	}
	// Evaluated strings are data, including any expression delimiters they contain.
	return shellValue{kind: shellValueEvaluated, value: &String{Value: name, Pos: run.Expression.Pos}}
}

// workflowScalarString applies the runner's scalar conversion for string schemas.
func workflowScalarString(value any) (string, bool) {
	switch value := value.(type) {
	case nil:
		return "", true
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), true
	default:
		return "", false
	}
}

func runnerPlatformLabels(runner *Runner) []*String {
	if runner == nil {
		return nil
	}
	var labels []*String
	for _, expression := range []*String{runner.Expression, runner.LabelsExpr} {
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
