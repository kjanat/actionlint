package typecheck

import (
	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func (sema *Checker) reportCallProblems(call *exprsyntax.FuncCallNode, problems []exprfunctions.CallProblemReport) {
	for _, report := range problems {
		if report.Demand == exprfunctions.DemandSkipped {
			continue
		}
		finding, phase := callProblemFinding(call, report.Problem)
		if phase == FailureStaticValidation {
			sema.validationError(finding)
		} else {
			sema.withEvaluationDemand(report.Demand, func() { sema.evaluationError(finding) })
		}
	}
}

// callProblemFinding attaches diagnostic identity and source ranges without
// changing checking state or evaluating the reported operation.
func callProblemFinding(call *exprsyntax.FuncCallNode, problem exprfunctions.CallProblem) (*Error, FailurePhase) {
	switch problem := problem.(type) {
	case exprfunctions.MissingFormatArgument:
		return errorfAtCallee(formatPlaceholder, call, "format string %q contains placeholder {%d} but only %d arguments are given to format", problem.Template, problem.Index, problem.Count), FailureEvaluation
	case exprfunctions.InvalidFormatPlaceholder:
		return errorfAtCallee(formatPlaceholder, call, "format string %q has an invalid placeholder", problem.Template), FailureEvaluation
	case exprfunctions.UnsupportedFormatSpecifier:
		return errorfAtCallee(formatPlaceholder, call, "format string %q uses unsupported format specifier %q", problem.Template, problem.Specifier), FailureEvaluation
	case exprfunctions.InvalidJSON:
		if problem.Offset != nil {
			return errorfAtExpr(invalidJSON, call.Args[0], "broken JSON string is passed to fromJSON() at offset %d: %s", *problem.Offset, problem.Message), FailureEvaluation
		}
		return errorfAtExpr(invalidJSON, call.Args[0], "broken JSON string is passed to fromJSON(): %s", problem.Message), FailureEvaluation
	case exprfunctions.JSONMemberCollision:
		return errorfAtExpr(jsonCollision, call.Args[0], "fromJSON() object members %q and %q collide under case-insensitive lookup at JSON offset %d; the selected value depends on JSON parsing mode", problem.First, problem.Second, problem.Offset), FailureStaticValidation
	case exprfunctions.InvalidCasePredicate:
		return errorfAtExpr(operandType, call.Args[problem.Index], "case() predicate must be boolean but found %s", problem.Actual.String()), FailureEvaluation
	}
	panic("unhandled function problem")
}
