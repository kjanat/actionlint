package checks

import (
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func uncancellableCondition(value declaration.StringView) []Finding {
	if !value.Present() {
		return nil
	}
	expr, err, offset := expression.ParseConditionExpression(value.Value())
	if err != nil || expr == nil || !execution.AlwaysIgnoresCancellation(expr) {
		return nil
	}
	finding := warning("uncancellable-condition", "", viewPosition(value.Pos()),
		"this job can continue after workflow cancellation. Review whether it must finish cancellation cleanup or reporting")
	// Fix only a standalone call. Compound conditions can deliberately select
	// cancellation-only work or have branches needing different cancellation behavior.
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
				Description: "Skip this job when cancelled (changes behavior)", Applicability: diagnostic.FixUnsafe,
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
