package correctness

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/model/contexts"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

// IfCondRule is the single definition of the if-cond rule.
var IfCondRule = &define.Rule{
	Name:        "if-cond",
	Description: "Report conditions that always evaluate to true or false",
	Category:    "correctness",
	Subcategory: "expressions",
	Recommended: true,
	Help:        "Correct the expression to test the intended runtime value. Remove a condition that is always true if it is unnecessary.",
	Examples: []rule.Example{rule.WorkflowExample("Remove an unconditional truthy condition.",
		"    if: true\n", "")},
	Checks: define.Fixed(define.Checks{Job: checkJobConditions, Step: checkStepCondition}),
}

func checkCondition(value *definition.String) []reporting.StepFinding {
	if value == nil {
		return nil
	}
	return reporting.FromDiagnostics(conditionDiagnostics(value.Value, sourcetext.ZeroIfNil(value.Pos)))
}

func checkJobConditions(job workflows.BoundJob) []reporting.StepFinding {
	out := checkCondition(job.If)
	if job.Snapshot != nil {
		out = append(out, checkCondition(job.Snapshot.If)...)
	}
	return out
}

func checkStepCondition(step workflows.BoundStep) []reporting.StepFinding {
	return checkCondition(step.If)
}

// conditionDiagnostics reports explicit if: conditions that are constant or have text around ${{ }}.
func conditionDiagnostics(value string, start sourcetext.Position) []results.Diagnostic {
	expr, err, _ := syntax.ParseConditionExpression(value)
	if err != nil {
		return nil
	}
	report := func(message string) []results.Diagnostic {
		return []results.Diagnostic{{Message: message, Start: start}}
	}
	if expr == nil {
		return report(fmt.Sprintf("if: condition %q is always evaluated to true because extra characters are around ${{ }}", value))
	}
	checker := typecheck.NewChecker(contexts.DefaultExpressionScope(), nil)
	if !checker.IsConstant(expr) {
		return nil
	}
	if _, errs := checker.Check(evaluate.EvaluateSubexpressions(expr)); len(errs) != 0 {
		return nil
	}
	if constant, known := evaluate.ConditionConstantValue(expr); known {
		text := strings.TrimSpace(syntax.ConditionSource(value))
		if strings.HasPrefix(text, "${{") {
			text = strings.TrimSpace(text[3 : len(text)-2])
		}
		if exprvalues.ExpressionTruthy(constant) {
			return report(fmt.Sprintf("constant expression %q in condition is always truthy. remove the if: section", text))
		}
		return report(fmt.Sprintf("constant expression %q in condition is always falsy. this step or job will be skipped", text))
	}
	return nil
}
