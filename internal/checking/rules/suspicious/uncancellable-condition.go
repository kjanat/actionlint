package suspicious

import (
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/applicability"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

var UncancellableConditionRule = &define.Rule{
	Name:        "uncancellable-condition",
	Description: "Review jobs that can continue after cancellation",
	Category:    "suspicious",
	Subcategory: "expressions",
	LegacyGroup: "correctness",
	Explanation: "Report job conditions using always() that can be true during cancellation. Cleanup and cancellation notices may need such conditions.",
	Help:        "Use !cancelled() if cancellation should stop the job.",
	Examples: []rule.Example{rule.WorkflowExample("",
		"    if: ${{ always() }}\n", "    if: ${{ !cancelled() }}\n")},
	DefaultLevel: "on",
	Checks:       define.Fixed(define.Checks{Job: checkJobCancellationCondition}),
}

func UncancellableCondition(w workflows.BoundWorkflow, value *definition.String) []reporting.StepFinding {
	if value == nil {
		return nil
	}
	expr, err, offset := syntax.ParseConditionExpression(value.Value)
	if err != nil || expr == nil || !conditions.AlwaysIgnoresCancellation(expr) {
		return nil
	}
	finding := reporting.Warning("uncancellable-condition", "", value.Pos,
		"this job can continue after workflow cancellation. Review whether it must finish cancellation cleanup or reporting")
	// Fix only a standalone call. Compound conditions can deliberately select
	// cancellation-only work or have branches needing different cancellation behavior.
	call, standalone := expr.(*syntax.FuncCallNode)
	source := w.ScalarSource(value)
	if standalone && strings.EqualFold(call.Callee, "always") && len(call.Args) == 0 && source != nil && syntax.ConditionSource(value.Value) == value.Value {
		span := syntax.ExpressionSpan(expr)
		startOffset, endOffset := offset+span.Start, offset+span.End
		startLine, startColumn := decodedConditionPosition(value.Value, startOffset)
		endLine, endColumn := decodedConditionPosition(value.Value, endOffset)
		start, startOK := source.Position(startLine, startColumn)
		end, endOK := source.EndPos(endLine, endColumn)
		replacement := "!cancelled()"
		if offset == 0 {
			// A bare leading ! is YAML tag syntax, so keep an expression wrapper.
			replacement = "${{ !cancelled() }}"
		}
		if startOK && endOK {
			finding.Fixes = []results.Fix{{
				Description: "Skip this job when cancelled (changes behavior)", Applicability: applicability.Unsafe,
				Edits: []results.Edit{{
					Start: sourcetext.ZeroIfNil(start), End: sourcetext.ZeroIfNil(end), Replacement: replacement,
				}},
			}}
		}
	}
	return []reporting.StepFinding{finding}
}

func decodedConditionPosition(value string, offset int) (line, column int) {
	prefix := value[:offset]
	return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
}

func checkJobCancellationCondition(job workflows.BoundJob) []reporting.StepFinding {
	return UncancellableCondition(job.Workflow(), job.If)
}
