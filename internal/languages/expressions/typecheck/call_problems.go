package typecheck

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

func (sema *Checker) reportCallProblems(call *syntax.FuncCallNode, problems []functions.CallProblemReport) {
	for _, report := range problems {
		if report.Demand == functions.DemandSkipped {
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
func callProblemFinding(call *syntax.FuncCallNode, problem functions.CallProblem) (*Error, FailurePhase) {
	switch problem := problem.(type) {
	case functions.MissingFormatArgument:
		return errorfAtCallee(formatPlaceholder, call, "format string %q contains placeholder {%d} but only %d arguments are given to format", problem.Template, problem.Index, problem.Count), FailureEvaluation
	case functions.InvalidFormatPlaceholder:
		return errorfAtCallee(formatPlaceholder, call, "format string %q has an invalid placeholder", problem.Template), FailureEvaluation
	case functions.UnsupportedFormatSpecifier:
		return errorfAtCallee(formatPlaceholder, call, "format string %q uses unsupported format specifier %q", problem.Template, problem.Specifier), FailureEvaluation
	case functions.InvalidJSON:
		if problem.Offset != nil {
			return errorfAtExpr(invalidJSON, call.Args[0], "broken JSON string is passed to fromJSON() at offset %d: %s", *problem.Offset, problem.Message), FailureEvaluation
		}
		return errorfAtExpr(invalidJSON, call.Args[0], "broken JSON string is passed to fromJSON(): %s", problem.Message), FailureEvaluation
	case functions.JSONMemberCollision:
		return errorfAtExpr(jsonCollision, call.Args[0], "fromJSON() object members %q and %q collide under case-insensitive lookup at JSON offset %d; the selected value depends on JSON parsing mode", problem.First, problem.Second, problem.Offset), FailureStaticValidation
	case functions.InvalidCasePredicate:
		return errorfAtExpr(operandType, call.Args[problem.Index], "case() predicate must be boolean but found %s", problem.Actual.String()), FailureEvaluation
	}
	panic("unhandled function problem")
}
