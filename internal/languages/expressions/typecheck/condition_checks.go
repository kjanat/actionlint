package typecheck

import (
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
)

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

func identityConditionReference(expr exprsyntax.Node) bool {
	switch exprsyntax.StaticReference(expr) {
	case "github.ref", "github.ref_name", "github.head_ref", "github.base_ref",
		"github.actor", "github.triggering_actor", "github.event.sender.login",
		"github.event.deployment.environment", "github.event.label.name",
		"github.event.pull_request.head.ref", "github.event.pull_request.base.ref":
		return true
	}
	return false
}

func (sema *Checker) checkConditionComparison(n *exprsyntax.CompareOpNode, left, right exprtypes.Type) {
	if !sema.condition {
		return
	}
	if sema.checks.MixedTypeComparisons &&
		(conditionStringReference(n.Left, left) && numericOrBooleanLiteral(n.Right) ||
			conditionStringReference(n.Right, right) && numericOrBooleanLiteral(n.Left)) {
		sema.ruleError(n, "mixed-type-comparisons", "condition compares a string to a number or boolean using Actions numeric coercion; compare strings explicitly or parse and validate the value with fromJSON()")
	}
	if sema.checks.CaseInsensitiveConditions && n.Kind.IsEqualityOp() {
		sema.checkIdentityComparison(n, n.Left, n.Right)
	}
}

func (sema *Checker) checkIdentityComparison(node, left, right exprsyntax.Node) {
	_, leftLiteral := left.(*exprsyntax.StringNode)
	_, rightLiteral := right.(*exprsyntax.StringNode)
	if identityConditionReference(left) && rightLiteral || identityConditionReference(right) && leftLiteral {
		sema.ruleError(node, "case-insensitive-conditions", "string operation in condition ignores case; case variants of this ref, identity, label or environment also match")
	}
}

func (sema *Checker) ruleError(node exprsyntax.Node, name, message string) {
	err := errorAtExpr(findingKind{name, name}, node, message)
	err.Advisory = true
	sema.errs = append(sema.errs, err)
}
