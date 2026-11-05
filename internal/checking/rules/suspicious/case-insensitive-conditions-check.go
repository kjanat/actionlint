package suspicious

import (
	"strings"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

// CaseInsensitiveConditions reports identity comparisons whose string operations ignore case.
// The caller invokes this rule only for workflow condition fields.
func CaseInsensitiveConditions(root exprsyntax.Node, types exprlang.InferredTypes, calls exprlang.CheckedCalls) []*exprlang.Error {
	var findings []*exprlang.Error
	report := func(node, left, right exprsyntax.Node) {
		_, leftLiteral := left.(*exprsyntax.StringNode)
		_, rightLiteral := right.(*exprsyntax.StringNode)
		if identityConditionReference(left) && rightLiteral || identityConditionReference(right) && leftLiteral {
			findings = append(findings, expressionFinding(node, "case-insensitive-conditions", "string operation in condition ignores case; case variants of this ref, identity, label or environment also match"))
		}
	}
	exprsyntax.Visit(root, func(node, _ exprsyntax.Node, entering bool) {
		if entering {
			return
		}
		switch node := node.(type) {
		case *exprsyntax.CompareOpNode:
			if _, checked := types.TypeOf(node); checked && node.Kind.IsEqualityOp() {
				report(node, node.Left, node.Right)
			}
		case *exprsyntax.FuncCallNode:
			if !calls.HasMatchedSignature(node) || len(node.Args) != 2 {
				return
			}
			switch strings.ToLower(node.Callee) {
			case "contains", "startswith", "endswith":
				report(node, node.Args[0], node.Args[1])
			}
		}
	})
	return findings
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
