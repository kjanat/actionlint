package expression

import "strings"

// StaticReference returns a normalized context path for static dot or literal
// bracket access. Computed selectors and expressions have no static reference.
func StaticReference(expr Node) string { return conditionReference(expr) }

// conditionReference accepts static dot and bracket access without guessing dynamic keys.
func conditionReference(expr Node) string {
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

func conditionStringReference(expr Node, ty Type) bool {
	path := conditionReference(expr)
	if path == "" {
		return false
	}
	if ty.Kind() == KindString {
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

func numericOrBooleanLiteral(expr Node) bool {
	switch expr.(type) {
	case *BoolNode, *IntNode, *FloatNode:
		return true
	default:
		return false
	}
}

func identityConditionReference(expr Node) bool {
	switch conditionReference(expr) {
	case "github.ref", "github.ref_name", "github.head_ref", "github.base_ref",
		"github.actor", "github.triggering_actor", "github.event.sender.login",
		"github.event.deployment.environment", "github.event.label.name",
		"github.event.pull_request.head.ref", "github.event.pull_request.base.ref":
		return true
	}
	return false
}

func (sema *Checker) checkConditionComparison(n *CompareOpNode, left, right Type) {
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

func (sema *Checker) checkIdentityComparison(node, left, right Node) {
	_, leftLiteral := left.(*StringNode)
	_, rightLiteral := right.(*StringNode)
	if identityConditionReference(left) && rightLiteral || identityConditionReference(right) && leftLiteral {
		sema.ruleError(node, "case-insensitive-conditions", "string operation in condition ignores case; case variants of this ref, identity, label or environment also match")
	}
}

func (sema *Checker) ruleError(node Node, name, message string) {
	err := errorAtExpr(findingKind{name, name}, node, message)
	sema.errs = append(sema.errs, err)
}
