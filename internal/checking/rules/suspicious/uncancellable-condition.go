package suspicious

import (
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"
)

// UncancellableConditionRule is the single definition of the uncancellable-condition rule.
var UncancellableConditionRule = &define.Rule{
	Name:        "uncancellable-condition",
	Description: "Review jobs that can continue after cancellation",
	Category:    "suspicious",
	Subcategory: "expressions",
	LegacyGroup: "correctness",
	Explanation: "Opt-in policy for job conditions using always() that can evaluate to true during cancellation. Cleanup and cancellation notifications can require this behavior; the condition alone does not prove a mistake. Step conditions are not reported because action names do not establish whether their work is needed for cleanup. cancel-timeout-minutes does not force cancellation of a job whose condition keeps it running. Warning when enabled; --strict enables this policy as an error, including intentional uses.",
	Help:        "If cancellation should stop the job, use !cancelled(). Keep always() when cancellation cleanup or reporting must run. A standalone always() offers a behavior-changing fix requiring explicit review; compound conditions have no replacement. See [GitHub's always() guidance](https://docs.github.com/en/actions/reference/workflows-and-actions/expressions#always) and the [job-versus-step cancellation test](https://github.com/kjanat/actionlint/actions/runs/41520665123).",
	Examples: []rule.Example{rule.WorkflowExample("When this job should stop on cancellation, use !cancelled(). Do not make this change for required cancellation cleanup or reporting.",
		"    if: ${{ always() }}\n", "    if: ${{ !cancelled() }}\n")},
	DefaultLevel: "on",
	Checks:       define.Fixed(define.Checks{Job: checkJobCancellationCondition}),
}

// UncancellableCondition reports a condition of w that keeps running after cancellation.
func UncancellableCondition(w workflows.Workflow, value *definition.String) []reporting.StepFinding {
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
				Description: "Skip this job when cancelled (changes behavior)", Applicability: results.FixUnsafe,
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

func checkJobCancellationCondition(job workflows.Job) []reporting.StepFinding {
	return UncancellableCondition(job.Workflow(), job.If)
}
