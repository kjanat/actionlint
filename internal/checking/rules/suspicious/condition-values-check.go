package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

// StringConditions reports a bare string reference used as a condition.
func StringConditions(root syntax.Node, types typecheck.InferredTypes) []*typecheck.Error {
	if ty, known := types.TypeOf(root); known && conditionStringReference(root, ty) {
		return []*typecheck.Error{expressionFinding(root, "string-conditions", "bare string condition is truthy for every non-empty value, including 'false' and '0'; compare the string explicitly or use fromJSON() to parse a boolean value")}
	}
	return nil
}

// MixedTypeComparisons reports string references compared with numeric or boolean literals.
func MixedTypeComparisons(root syntax.Node, types typecheck.InferredTypes) []*typecheck.Error {
	var findings []*typecheck.Error
	syntax.Visit(root, func(node, _ syntax.Node, entering bool) {
		if !entering {
			return
		}
		comparison, ok := node.(*syntax.CompareOpNode)
		if !ok {
			return
		}
		left, leftKnown := types.TypeOf(comparison.Left)
		right, rightKnown := types.TypeOf(comparison.Right)
		if !leftKnown || !rightKnown {
			return
		}
		if conditionStringReference(comparison.Left, left) && numericOrBooleanLiteral(comparison.Right) || conditionStringReference(comparison.Right, right) && numericOrBooleanLiteral(comparison.Left) {
			findings = append(findings, expressionFinding(comparison, "mixed-type-comparisons", "condition compares a string to a number or boolean using Actions numeric coercion; compare strings explicitly or parse and validate the value with fromJSON()"))
		}
	})
	return findings
}

func conditionStringReference(expr syntax.Node, ty exprtypes.Type) bool {
	path := syntax.StaticReference(expr)
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

func numericOrBooleanLiteral(expr syntax.Node) bool {
	switch expr.(type) {
	case *syntax.BoolNode, *syntax.IntNode, *syntax.FloatNode:
		return true
	default:
		return false
	}
}
