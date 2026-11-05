package suspicious

import (
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// StringConditions reports a bare string reference used as a condition.
func StringConditions(root exprsyntax.Node, types exprlang.InferredTypes) []*exprlang.Error {
	if ty, known := types.TypeOf(root); known && conditionStringReference(root, ty) {
		return []*exprlang.Error{conditionFinding(root, "string-conditions", "bare string condition is truthy for every non-empty value, including 'false' and '0'; use an explicit string comparison or fromJSON() for a boolean contract")}
	}
	return nil
}

// MixedTypeComparisons reports string references compared with numeric or boolean literals.
func MixedTypeComparisons(root exprsyntax.Node, types exprlang.InferredTypes) []*exprlang.Error {
	var findings []*exprlang.Error
	exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
		if !entering {
			return
		}
		comparison, ok := node.(*exprsyntax.CompareOpNode)
		if !ok {
			return
		}
		left, leftKnown := types.TypeOf(comparison.Left)
		right, rightKnown := types.TypeOf(comparison.Right)
		if !leftKnown || !rightKnown {
			return
		}
		if conditionStringReference(comparison.Left, left) && numericOrBooleanLiteral(comparison.Right) || conditionStringReference(comparison.Right, right) && numericOrBooleanLiteral(comparison.Left) {
			findings = append(findings, conditionFinding(comparison, "mixed-type-comparisons", "condition compares a string to a number or boolean using Actions numeric coercion; compare strings explicitly or parse and validate the value with fromJSON()"))
		}
	})
	return findings
}

func conditionFinding(node exprsyntax.Node, name, message string) *exprlang.Error {
	span := exprsyntax.ExpressionSpan(node)
	return &exprlang.Error{Rule: name, Code: name, Message: message, Offset: span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column, Advisory: true}
}

func conditionStringReference(expr exprsyntax.Node, ty exprtypes.Type) bool {
	path := exprsyntax.StaticReference(expr)
	if path == "" {
		return false
	}
	if ty.Kind() == exprtypes.KindString {
		return true
	}
	// Webhook payloads are otherwise open objects; these documented text fields
	// have a stable type even when no complete event schema is available.
	switch path {
	case "github.event.comment.body", "github.event.issue.title", "github.event.issue.body",
		"github.event.pull_request.title", "github.event.pull_request.body",
		"github.event.deployment.environment", "github.event.label.name":
		return true
	}
	return false
}

func numericOrBooleanLiteral(expr exprsyntax.Node) bool {
	switch expr.(type) {
	case *exprsyntax.BoolNode, *exprsyntax.IntNode, *exprsyntax.FloatNode:
		return true
	default:
		return false
	}
}
