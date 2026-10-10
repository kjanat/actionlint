package actionlint

import "strings"

func enabledPolicy(value *bool) bool { return value != nil && *value }

// conditionReference accepts static dot and bracket access without guessing dynamic keys.
func conditionReference(expr ExprNode) string {
	switch n := expr.(type) {
	case *VariableNode:
		return strings.ToLower(n.Name)
	case *ObjectDerefNode:
		if receiver := conditionReference(n.Receiver); receiver != "" {
			return receiver + "." + strings.ToLower(n.Property)
		}
	case *IndexAccessNode:
		if key, ok := n.Index.(*StringNode); ok {
			if receiver := conditionReference(n.Operand); receiver != "" {
				return receiver + "." + strings.ToLower(key.Value)
			}
		}
	}
	return ""
}

func conditionStringReference(expr ExprNode, ty ExprType) bool {
	path := conditionReference(expr)
	if path == "" {
		return false
	}
	if _, ok := ty.(StringType); ok {
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

func numericOrBooleanLiteral(expr ExprNode) bool {
	switch expr.(type) {
	case *BoolNode, *IntNode, *FloatNode:
		return true
	default:
		return false
	}
}

func identityConditionReference(expr ExprNode) bool {
	path := conditionReference(expr)
	switch path {
	case "github.ref", "github.ref_name", "github.head_ref", "github.base_ref",
		"github.actor", "github.triggering_actor", "github.event.sender.login",
		"github.event.deployment.environment", "github.event.label.name",
		"github.event.pull_request.head.ref", "github.event.pull_request.base.ref":
		return true
	}
	return false
}

func (sema *ExprSemanticsChecker) checkConditionComparison(n *CompareOpNode, left, right ExprType) {
	if !sema.condition {
		return
	}
	if enabledPolicy(sema.policy.MixedTypeComparisons) &&
		(conditionStringReference(n.Left, left) && numericOrBooleanLiteral(n.Right) ||
			conditionStringReference(n.Right, right) && numericOrBooleanLiteral(n.Left)) {
		sema.errorf(n, "condition compares a string to a number or boolean using Actions numeric coercion; compare strings explicitly or parse and validate the value with fromJSON() (policy: mixed-type-comparisons)")
	}
	if enabledPolicy(sema.policy.CaseInsensitiveConditions) && n.Kind.IsEqualityOp() {
		sema.checkIdentityComparison(n, n.Left, n.Right)
	}
}

func (sema *ExprSemanticsChecker) checkIdentityComparison(node, left, right ExprNode) {
	_, leftLiteral := left.(*StringNode)
	_, rightLiteral := right.(*StringNode)
	if identityConditionReference(left) && rightLiteral || identityConditionReference(right) && leftLiteral {
		sema.errorf(node, "string operation in condition ignores case; case variants of this ref, identity, label or environment also match. validate case-sensitive identities before using the result as a gate (policy: case-insensitive-conditions)")
	}
}
