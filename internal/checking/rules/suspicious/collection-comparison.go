package suspicious

import (
	"slices"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

func CollectionComparisonDeclaration() rule.Declaration {
	return rule.Declaration{
		Name: "collection-comparison", Description: "Detect equality checks between object or array instances",
		Category: "suspicious", Subcategory: "expressions", Maturity: "stable", Provider: "actionlint",
		Recommended: true, DefaultLevel: "error", SupportsActions: true, Scope: rule.CompositeConfiguration,
		Explanation: "Objects and arrays compare equal only when they are the same instance. " +
			"Equal contents do not make separately created objects or arrays equal. " +
			"A condition that grants access or gates a write can therefore evaluate differently from a comparison of contents. " +
			"This check reports == and != between two known objects or two known arrays. " +
			"It accepts null checks and repeated access to the same context property, and skips unknown types and expressions that cannot execute.",
		Help:             "Compare the fields or array elements that determine equality. Disable this rule for a deliberate instance-identity comparison.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#collection-comparison",
		Examples: []rule.Example{rule.WorkflowExample("Compare the object properties that matter to the condition.",
			`    if: ${{ fromJSON('{"ready":true}') == fromJSON('{"ready":true}') }}`+"\n",
			`    if: ${{ fromJSON('{"ready":true}').ready == fromJSON('{"ready":true}').ready }}`+"\n")},
	}
}

// CollectionComparison reports equality comparisons between object or array instances.
func CollectionComparison(root exprsyntax.Node, types exprlang.InferredTypes, evaluation exprlang.EvaluationResults) []*exprlang.Error {
	var findings []*exprlang.Error
	exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
		comparison, ok := node.(*exprsyntax.CompareOpNode)
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
		leftPath, leftStatic := exprsyntax.ReferencePath(comparison.Left)
		rightPath, rightStatic := exprsyntax.ReferencePath(comparison.Right)
		if leftStatic && rightStatic && slices.Equal(leftPath, rightPath) {
			return
		}
		findings = append(findings, expressionFinding(comparison, "collection-comparison",
			"this comparison checks object or array instance identity. Compare the relevant fields or elements to check their contents"))
	})
	return findings
}
