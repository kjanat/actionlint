package correctness

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprcheck "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

func checkCondition(value *workflowlang.String) []reporting.Finding {
	if value == nil {
		return nil
	}
	return reporting.FromDiagnostics(conditionDiagnostics(value.Value, reporting.Position(value.Pos)))
}

func checkJobConditions(job declaration.Job) []reporting.Finding {
	out := checkCondition(job.If)
	if job.Snapshot != nil {
		out = append(out, checkCondition(job.Snapshot.If)...)
	}
	return out
}

func checkStepCondition(step declaration.Step) []reporting.Finding {
	return checkCondition(step.If)
}

// conditionDiagnostics reports explicit if: conditions that are constant or have text around ${{ }}.
func conditionDiagnostics(value string, start sourcepos.Position) []results.Diagnostic {
	expr, err, _ := exprsyntax.ParseConditionExpression(value)
	if err != nil {
		return nil
	}
	report := func(message string) []results.Diagnostic {
		return []results.Diagnostic{{Message: message, Start: start}}
	}
	if expr == nil {
		return report(fmt.Sprintf("if: condition %q is always evaluated to true because extra characters are around ${{ }}", value))
	}
	checker := exprcheck.NewChecker(contextspec.DefaultExpressionScope(), nil)
	if !checker.IsConstant(expr) {
		return nil
	}
	if _, errs := checker.Check(expreval.EvaluateSubexpressions(expr)); len(errs) != 0 {
		return nil
	}
	if constant, known := expreval.ConditionConstantValue(expr); known {
		text := strings.TrimSpace(exprsyntax.ConditionSource(value))
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
