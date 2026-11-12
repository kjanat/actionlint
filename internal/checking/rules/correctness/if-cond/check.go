// Package condition checks explicit condition expressions without visitor lifecycle state.
package condition

import (
	"fmt"
	"strings"

	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprcheck "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/results"
)

func Check(value string, start sourcepos.Position) []results.Diagnostic {
	expr, err, _ := exprsyntax.ParseConditionExpression(value)
	if err != nil {
		return nil
	}
	report := func(message string) []results.Diagnostic {
		return []results.Diagnostic{{Rule: "if-cond", Message: message, Start: start}}
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
