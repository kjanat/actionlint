package checks

import (
	"math"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

func uncancellableCondition(value declaration.StringView, subject string) []Finding {
	if !value.Present() {
		return nil
	}
	expr, err, offset := expression.ParseConditionExpression(value.Value())
	if err != nil || expr == nil || !execution.AlwaysIgnoresCancellation(expr) {
		return nil
	}
	finding := warning("uncancellable-condition", "", viewPosition(value.Pos()),
		"always() can prevent this %s from stopping when the workflow is cancelled. Use !cancelled() to run after failures but stop when cancelled", subject)
	// Fix only a standalone call. Substituting inside an OR expression does
	// not necessarily establish a cancellation guard. Folded expression strings
	// have no direct token mapping to their original source.
	call, standalone := expr.(*expression.FuncCallNode)
	source := value.ExpressionSource()
	if standalone && strings.EqualFold(call.Callee, "always") && len(call.Args) == 0 && source.Present() && expression.ConditionSource(value.Value()) == value.Value() {
		span := expression.ExpressionSpan(expr)
		startOffset, endOffset := offset+span.Start, offset+span.End
		startLine, startColumn := decodedConditionPosition(value.Value(), startOffset)
		endLine, endColumn := decodedConditionPosition(value.Value(), endOffset)
		start, startOK := source.Position(startLine, startColumn)
		end, endOK := source.EndPos(endLine, endColumn)
		replacement := "!cancelled()"
		if offset == 0 {
			// A bare leading ! is YAML tag syntax, so keep an expression wrapper.
			replacement = "${{ !cancelled() }}"
		}
		if startOK && endOK {
			finding.Fixes = []diagnostic.Fix{{
				Description: "Respect cancellation (changes cancellation behavior)", Applicability: "suggested",
				Edits: []diagnostic.Edit{{
					Start: position(&start), End: position(&end), Replacement: replacement,
				}},
			}}
		}
	}
	return []Finding{finding}
}

func decodedConditionPosition(value string, offset int) (line, column int) {
	prefix := value[:offset]
	return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
}

func uncancellableCheckout(step declaration.StepView) []Finding {
	action, ok := step.Action()
	if !ok || action.Uses().ContainsExpression() {
		return nil
	}
	owner, repo, ref, problem := references.SplitAction(action.Uses().Value())
	if problem != "" || ref == "" || !strings.EqualFold(owner, "actions") || !strings.EqualFold(repo, "checkout") {
		return nil
	}
	out := uncancellableCondition(step.If(), "checkout step")
	for i := range out {
		out[i].Step = step
		if boundedCheckoutCancellation(step.Job()) {
			out[i].Severity = "info"
			out[i].Message = "always() lets this checkout step run even when cancelled. cancel-timeout-minutes limits cleanup time, but !cancelled() skips checkout on cancellation"
		}
	}
	return out
}

func boundedCheckoutCancellation(job declaration.JobView) bool {
	timeout := job.CancelTimeoutMinutes()
	if !timeout.Present() {
		return false
	}
	minutes := timeout.Value()
	if timeout.Expression().Present() {
		constant, known := expressionLiteral(timeout.Expression())
		if !known {
			return false
		}
		switch number := constant.(type) {
		case int:
			minutes = float64(number)
		case float64:
			minutes = number
		default:
			return false
		}
	}
	if minutes <= 0 || math.IsNaN(minutes) || math.IsInf(minutes, 0) {
		return false
	}
	if job.If().Present() {
		expr, err, _ := expression.ParseConditionExpression(job.If().Value())
		if err != nil || expr == nil || execution.CanRunDuringCancellation(expr) {
			return false
		}
	}
	return true
}
