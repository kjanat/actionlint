package action

import (
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

func expressionCoercionFindings(expressions []actionmeta.ParsedExpression, field string) []actionmeta.ExpressionProblem {
	var findings []actionmeta.ExpressionProblem
	for _, expression := range expressions {
		checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
		checker.SetContextAvailability(actionmeta.AllowedContexts(field))
		contextspec.MergeInputTypes(checker, exprtypes.NewMapObjectType(exprtypes.StringType()))
		checker.SetContextType("steps", exprtypes.NewMapObjectType(exprtypes.AnyType()).Type())
		result := checker.CheckResult(expression.Node)
		for _, finding := range suspicious.SuspiciousCoercion(expression.Node, result.Types, result.Calls) {
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
