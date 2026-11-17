package suspicious

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// CollectionComparisonRule is the single definition of the collection-comparison rule.
var CollectionComparisonRule = &define.Rule{
	Name:            "collection-comparison",
	Description:     "Detect equality checks between object or array instances",
	Category:        "suspicious",
	Subcategory:     "expressions",
	Recommended:     true,
	DefaultLevel:    "error",
	SupportsActions: true,
	Scope:           rule.CompositeConfiguration,
	Explanation: "Objects and arrays compare equal only when they are the same instance. " +
		"Equal contents do not make separately created objects or arrays equal. " +
		"A condition that grants access or gates a write can therefore evaluate differently from a comparison of contents. " +
		"This check reports == and != between two known objects or two known arrays. " +
		"It accepts null checks and repeated access to the same context property, and skips unknown types and expressions that cannot execute.",
	Help: "Compare the fields or array elements that determine equality. Disable this rule for a deliberate instance-identity comparison.",
	Examples: []rule.Example{rule.WorkflowExample("Compare the object properties that matter to the condition.",
		`    if: ${{ fromJSON('{"ready":true}') == fromJSON('{"ready":true}') }}`+"\n",
		`    if: ${{ fromJSON('{"ready":true}').ready == fromJSON('{"ready":true}').ready }}`+"\n")},
	DeclaredBy: "expression",
}

// CollectionComparison reports equality comparisons between object or array instances.
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
