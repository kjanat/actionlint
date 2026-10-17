package engine

import (
	"strings"

	"actionlint.kjanat.dev/internal/workflow/schema"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// implicit distinguishes the runner's bash -e fallback from explicit bash,
// which also enables pipefail. Custom templates supply their own options.
type executionShell struct {
	name     string
	implicit bool
}

func resolveRunShell(exec *workflowlang.ExecRun, jobShell, workflowShell, runnerShell shellValue) executionShell {
	for _, candidate := range []shellValue{shellValueFromString(exec.Shell), jobShell, workflowShell} {
		candidate = executionShellLiteral(candidate)
		switch candidate.kind {
		case shellValueSource, shellValueEvaluated:
			if candidate.value.Value != "" {
				return executionShell{name: candidate.value.Value}
			}
		case shellValueUnknown:
			return executionShell{}
		case shellValueUnspecified:
			continue
		}
	}
	if runnerShell.kind == shellValueUnknown {
		return executionShell{}
	}
	if runnerShell.value != nil {
		return executionShell{name: runnerShell.value.Value, implicit: true}
	}
	// Static analysis cannot discover whether a remote runner lacks Bash.
	return executionShell{name: "bash", implicit: true}
}

func executionShellLiteral(value shellValue) shellValue {
	if value.kind != shellValueSource || !value.value.ContainsExpression() {
		return value
	}
	if literal, known := workflowlang.WorkflowExpressionLiteral(value.value); known {
		if text, ok := workflowlang.WorkflowScalarString(literal); ok {
			return shellValue{kind: shellValueEvaluated, value: &workflowlang.String{Value: text}}
		}
	}
	// A custom template can have a known executable and dynamic arguments.
	command, _, _ := strings.Cut(value.value.Value, " ")
	if !strings.Contains(command, "${{") {
		return value
	}
	return shellValue{kind: shellValueUnknown}
}

func containerShell(container *workflowlang.Container) shellValue {
	if container == nil {
		return shellValue{}
	}
	image := shellValueFromString(container.Image)
	if container.Expression != nil {
		value, known := workflowlang.WorkflowExpressionLiteral(container.Expression)
		if !known {
			return shellValue{kind: shellValueUnknown}
		}
		if object, ok := value.(map[string]any); ok {
			value = schema.Property(object, "image")
		}
		text, ok := workflowlang.WorkflowScalarString(value)
		if !ok {
			return shellValue{kind: shellValueUnknown}
		}
		image = shellValue{kind: shellValueEvaluated, value: &workflowlang.String{Value: text}}
	} else if image.kind == shellValueSource && image.value.ContainsExpression() {
		value, known := workflowlang.WorkflowExpressionLiteral(image.value)
		text, scalar := workflowlang.WorkflowScalarString(value)
		if !known || !scalar {
			return shellValue{kind: shellValueUnknown}
		}
		image = shellValue{kind: shellValueEvaluated, value: &workflowlang.String{Value: text}}
	}
	if image.value == nil || image.value.Value == "" {
		return shellValue{}
	}
	return shellValueFromString(&workflowlang.String{Value: "sh"})
}
