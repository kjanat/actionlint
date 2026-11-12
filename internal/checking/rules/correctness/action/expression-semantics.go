package action

import (
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"
	contextspec "actionlint.kjanat.dev/v2/internal/model/contexts"
	"actionlint.kjanat.dev/v2/internal/model/contexts/outputs"

	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

// expressionSemanticFindings checks metadata expressions using the declaration's
// types. Values from individual workflow calls are unavailable here.
func expressionSemanticFindings(expressions []actionmeta.ParsedExpression, field string, advisories bool) []actionmeta.ExpressionProblem {
	var findings []actionmeta.ExpressionProblem
	for _, expression := range expressions {
		checker := exprlang.NewChecker(contextspec.DefaultExpressionScope(), nil)
		checker.SetContextAvailability(actionmeta.AllowedContexts(field))
		contextspec.MergeInputTypes(checker, exprtypes.NewMapObjectType(exprtypes.StringType()))
		// Output names and values are unknown here. Each output is a string;
		// the outputs property is an object.
		step := outputs.StepType(exprtypes.NewMapObjectType(exprtypes.StringType()))
		checker.SetContextType("steps", exprtypes.NewMapObjectType(step.Type()).Type())
		evaluation := expreval.EvaluateSubexpressions(expression.Node)
		result := checker.CheckResult(evaluation)
		semantic := result.Findings
		if advisories {
			semantic = append(semantic, suspicious.SuspiciousCoercion(expression.Node, result.Types, result.Calls)...)
			semantic = append(semantic, suspicious.JoinLiteral(expression.Node, result.Calls)...)
			semantic = append(semantic, suspicious.CollectionComparison(expression.Node, result.Types, evaluation)...)
			if strings.HasSuffix(field, ".if") {
				semantic = append(semantic, suspicious.StringConditions(expression.Node, result.Types)...)
			}
		}
		for _, finding := range semantic {
			var code actionmeta.ProblemCode
			switch finding.Rule {
			case "coercion":
				code = actionmeta.CodeCoercion
			case "string-conditions":
				code = actionmeta.CodeStringCondition
			case "join-literal":
				code = actionmeta.CodeJoinLiteral
			case "collection-comparison":
				code = actionmeta.CodeCollectionComparison
			case "expression-call":
				switch finding.Code {
				case "invalid-json":
					code = actionmeta.CodeInvalidJSON
				case "format-placeholder":
					code = actionmeta.CodeFormatPlaceholder
				}
			}
			// ReadExpressions already checks syntax, names, context availability,
			// argument counts and duplicate JSON members.
			if code == "" {
				continue
			}
			problem := actionmeta.ExpressionProblem{Code: code, Message: finding.Message}
			if expression.Offset != 0 {
				problem.Offset = expression.Offset + finding.Offset
				problem.EndOffset = expression.Offset + finding.EndOffset
			}
			findings = append(findings, problem)
		}
	}
	return findings
}
