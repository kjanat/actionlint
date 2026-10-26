package execution

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
)

// ShellKind distinguishes absent, source, evaluated and unknown overrides.
type ShellKind uint8

const (
	ShellUnspecified ShellKind = iota
	ShellSource
	ShellEvaluated
	ShellUnknown
)

// ShellValue distinguishes an absent shell from an unresolved override.
type ShellValue struct {
	Kind  ShellKind
	Value *workflowlang.String
}

func ShellValueOf(value *workflowlang.String) ShellValue {
	if value == nil {
		return ShellValue{}
	}
	return ShellValue{Kind: ShellSource, Value: value}
}

// DefaultShell resolves a declared defaults.run shell without treating expression results as templates.
func DefaultShell(defaults *workflowlang.Defaults) ShellValue {
	if defaults == nil || defaults.Run == nil {
		return ShellValue{}
	}
	run := defaults.Run
	if run.Expression == nil {
		return ShellValueOf(run.Shell)
	}
	value, known := workflowlang.WorkflowExpressionLiteral(run.Expression)
	if !known || len(schema.LiteralErrors(schema.DefaultsRun, value, "defaults.run")) != 0 {
		return ShellValue{Kind: ShellUnknown}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ShellValue{Kind: ShellUnknown}
	}
	shell := schema.Property(object, "shell")
	if shell == nil {
		return ShellValue{}
	}
	name, ok := workflowlang.WorkflowScalarString(shell)
	if !ok {
		return ShellValue{Kind: ShellUnknown}
	}
	// Evaluated strings are data, including any expression delimiters they contain.
	return ShellValue{Kind: ShellEvaluated, Value: &workflowlang.String{Value: name, Pos: run.Expression.Pos}}
}
