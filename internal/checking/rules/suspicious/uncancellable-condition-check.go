package suspicious

import (
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	"actionlint.kjanat.dev/v2/internal/results"
)

func UncancellableCondition(value declaration.StringView) []reporting.Finding {
	if !value.Present() {
		return nil
	}
	expr, err, offset := exprsyntax.ParseConditionExpression(value.Value())
	if err != nil || expr == nil || !execution.AlwaysIgnoresCancellation(expr) {
		return nil
	}
	finding := reporting.Warning("uncancellable-condition", "", reporting.ViewPosition(value.Pos()),
		"this job can continue after workflow cancellation. Review whether it must finish cancellation cleanup or reporting")
	// Fix only a standalone call. Compound conditions can deliberately select
	// cancellation-only work or have branches needing different cancellation behavior.
	call, standalone := expr.(*exprsyntax.FuncCallNode)
	source := value.ExpressionSource()
	if standalone && strings.EqualFold(call.Callee, "always") && len(call.Args) == 0 && source.Present() && exprsyntax.ConditionSource(value.Value()) == value.Value() {
		span := exprsyntax.ExpressionSpan(expr)
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
			finding.Fixes = []results.Fix{{
				Description: "Skip this job when cancelled (changes behavior)", Applicability: results.FixUnsafe,
				Edits: []results.Edit{{
					Start: reporting.Position(&start), End: reporting.Position(&end), Replacement: replacement,
				}},
			}}
		}
	}
	return []reporting.Finding{finding}
}

func decodedConditionPosition(value string, offset int) (line, column int) {
	prefix := value[:offset]
	return strings.Count(prefix, "\n") + 1, utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
}

func checkJobCancellationCondition(job declaration.JobView) []reporting.Finding {
	return UncancellableCondition(job.If())
}
