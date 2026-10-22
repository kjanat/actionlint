package execution

import (
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
)

// Shell distinguishes the runner's bash -e fallback from explicit bash,
// which also enables pipefail. Custom templates supply their own options.
type Shell struct {
	Name     string
	Implicit bool
}

// ResolveShell applies step, job, workflow and runner shell precedence.
func ResolveShell(exec *workflowlang.ExecRun, jobShell, workflowShell, runnerShell ShellValue) Shell {
	for _, candidate := range []ShellValue{ShellValueOf(exec.Shell), jobShell, workflowShell} {
		candidate = executionShellLiteral(candidate)
		switch candidate.Kind {
		case ShellSource, ShellEvaluated:
			if candidate.Value.Value != "" {
				return Shell{Name: candidate.Value.Value}
			}
		case ShellUnknown:
			return Shell{}
		case ShellUnspecified:
			continue
		}
	}
	if runnerShell.Kind == ShellUnknown {
		return Shell{}
	}
	if runnerShell.Value != nil {
		return Shell{Name: runnerShell.Value.Value, Implicit: true}
	}
	// Static analysis cannot discover whether a remote runner lacks Bash.
	return Shell{Name: "bash", Implicit: true}
}

func executionShellLiteral(value ShellValue) ShellValue {
	if value.Kind != ShellSource || !value.Value.ContainsExpression() {
		return value
	}
	if literal, known := workflowlang.WorkflowExpressionLiteral(value.Value); known {
		if text, ok := workflowlang.WorkflowScalarString(literal); ok {
			return ShellValue{Kind: ShellEvaluated, Value: &workflowlang.String{Value: text}}
		}
	}
	// A custom template can have a known executable and dynamic arguments.
	command, _, _ := strings.Cut(value.Value.Value, " ")
	if !strings.Contains(command, "${{") {
		return value
	}
	return ShellValue{Kind: ShellUnknown}
}

// ContainerShell resolves a known job container's default shell.
func ContainerShell(container *workflowlang.Container) ShellValue {
	if container == nil {
		return ShellValue{}
	}
	image := ShellValueOf(container.Image)
	if container.Expression != nil {
		value, known := workflowlang.WorkflowExpressionLiteral(container.Expression)
		if !known {
			return ShellValue{Kind: ShellUnknown}
		}
		if object, ok := value.(map[string]any); ok {
			value = schema.Property(object, "image")
		}
		text, ok := workflowlang.WorkflowScalarString(value)
		if !ok {
			return ShellValue{Kind: ShellUnknown}
		}
		image = ShellValue{Kind: ShellEvaluated, Value: &workflowlang.String{Value: text}}
	} else if image.Kind == ShellSource && image.Value.ContainsExpression() {
		value, known := workflowlang.WorkflowExpressionLiteral(image.Value)
		text, scalar := workflowlang.WorkflowScalarString(value)
		if !known || !scalar {
			return ShellValue{Kind: ShellUnknown}
		}
		image = ShellValue{Kind: ShellEvaluated, Value: &workflowlang.String{Value: text}}
	}
	if image.Value == nil || image.Value.Value == "" {
		return ShellValue{}
	}
	return ShellValueOf(&workflowlang.String{Value: "sh"})
}
