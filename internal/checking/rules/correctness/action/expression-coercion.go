package action

import (
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

func expressionCoercionFindings(expressions []actionmeta.ParsedExpression, field string) []actionmeta.ExpressionProblem {
	var findings []actionmeta.ExpressionProblem
	for _, expression := range expressions {
		checker := exprlang.NewChecker(&exprlang.Options{Checks: exprlang.Checks{SuspiciousCoercion: true}})
		checker.SetContextAvailability(actionmeta.AllowedContexts(field))
		checker.UpdateInputs(exprtypes.NewMapObjectType(exprtypes.StringType()))
		checker.UpdateSteps(exprtypes.NewMapObjectType(exprtypes.AnyType()))
		result := checker.CheckResult(expression.Node)
		for _, finding := range result.Findings {
			if !finding.Advisory || finding.Code != string(actionmeta.CodeCoercion) {
				continue
			}
			problem := actionmeta.ExpressionProblem{Code: actionmeta.CodeCoercion, Message: finding.Message}
			if expression.Offset != 0 {
				problem.Offset = expression.Offset + finding.Offset
				problem.EndOffset = expression.Offset + finding.EndOffset
			}
			findings = append(findings, problem)
		}
	}
	return findings
}
