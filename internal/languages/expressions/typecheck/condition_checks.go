package typecheck

import (
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

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

func (sema *Checker) checkConditionComparison(n *exprsyntax.CompareOpNode) {
	if !sema.condition {
		return
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
