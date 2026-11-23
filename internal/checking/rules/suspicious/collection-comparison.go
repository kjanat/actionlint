package suspicious

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var CollectionComparisonRule = &define.Rule{
	Name:            "collection-comparison",
	Description:     "Detect equality checks between object or array instances",
	Category:        "suspicious",
	Subcategory:     "expressions",
	Recommended:     true,
	DefaultLevel:    "error",
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	Explanation: "Report == and != between two known objects or two known arrays. " +
		"Objects and arrays are equal only when they are the same instance, even if their contents match.",
	Help: "Compare the fields or elements that decide equality.",
	Examples: []rule.Example{rule.WorkflowExample("",
		`    if: ${{ fromJSON('{"ready":true}') == fromJSON('{"ready":true}') }}`+"\n",
		`    if: ${{ fromJSON('{"ready":true}').ready == fromJSON('{"ready":true}').ready }}`+"\n")},
	DeclaredBy: "expression",
}

func CollectionComparison(root syntax.Node, types typecheck.InferredTypes, evaluation typecheck.EvaluationResults) []*typecheck.Error {
	var findings []*typecheck.Error
	syntax.Visit(root, func(node, _ syntax.Node, entering bool) {
		comparison, ok := node.(*syntax.CompareOpNode)
		if !entering || !ok || !comparison.Kind.IsEqualityOp() {
			return
		}
		if _, reachable := evaluation.Result(comparison); !reachable {
			return
		}
		left, leftKnown := types.TypeOf(comparison.Left)
		right, rightKnown := types.TypeOf(comparison.Right)
		if !leftKnown || !rightKnown || left.Kind() != right.Kind() || left.Kind() != exprtypes.KindObject && left.Kind() != exprtypes.KindArray {
			return
		}
		leftPath, leftStatic := syntax.ReferencePath(comparison.Left)
		rightPath, rightStatic := syntax.ReferencePath(comparison.Right)
		if leftStatic && rightStatic && slices.Equal(leftPath, rightPath) {
			return
		}
		findings = append(findings, expressionFinding(comparison, "collection-comparison",
			"this comparison checks object or array instance identity. Compare the relevant fields or elements to check their contents"))
	})
	return findings
}
