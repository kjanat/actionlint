// Package condition checks explicit condition expressions without visitor lifecycle state.
package condition

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/expression"
)

func Check(value string, start diagnostic.Position) []diagnostic.Diagnostic {
	expr, err, _ := expression.ParseConditionExpression(value)
	if err != nil {
		return nil
	}
	report := func(message string) []diagnostic.Diagnostic {
		return []diagnostic.Diagnostic{{Rule: "if-cond", Message: message, Start: start}}
	}
	if expr == nil {
		return report(fmt.Sprintf("if: condition %q is always evaluated to true because extra characters are around ${{ }}", value))
	}
	checker := expression.NewChecker(false, nil)
	if !checker.IsConstant(expr) {
		return nil
	}
	if _, errs := checker.Check(expr); len(errs) != 0 {
		return nil
	}
	if constant, known := expression.ConditionConstantValue(expr); known {
		text := strings.TrimSpace(expression.ConditionSource(value))
		if strings.HasPrefix(text, "${{") {
			text = strings.TrimSpace(text[3 : len(text)-2])
		}
		if expression.ExpressionTruthy(constant) {
			return report(fmt.Sprintf("constant expression %q in condition is always truthy. remove the if: section", text))
		}
		return report(fmt.Sprintf("constant expression %q in condition is always falsy. this step or job will be skipped", text))
	}
	return nil
}
