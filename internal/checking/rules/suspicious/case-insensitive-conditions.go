package suspicious

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

// CaseInsensitiveConditionsRule is the single definition of the case-insensitive-conditions rule.
var CaseInsensitiveConditionsRule = &define.Rule{
	Name:        "case-insensitive-conditions",
	Description: "Flag case-insensitive identity comparisons",
	Category:    "suspicious",
	Subcategory: "expressions",
	Help:        "Expression string comparisons ignore case; avoid using them as case-sensitive identity checks.",
	DeclaredBy:  "expression",
}

// CaseInsensitiveConditions reports identity comparisons whose string operations ignore case.
// The caller invokes this rule only for workflow condition fields.
func CaseInsensitiveConditions(root syntax.Node, types typecheck.InferredTypes, calls typecheck.CheckedCalls) []*typecheck.Error {
	var findings []*typecheck.Error
	report := func(node, left, right syntax.Node) {
		_, leftLiteral := left.(*syntax.StringNode)
		_, rightLiteral := right.(*syntax.StringNode)
		if identityConditionReference(left) && rightLiteral || identityConditionReference(right) && leftLiteral {
			findings = append(findings, expressionFinding(node, "case-insensitive-conditions", "string operation in condition ignores case; case variants of this ref, identity, label or environment also match"))
		}
	}
	syntax.Visit(root, func(node, _ syntax.Node, entering bool) {
		if entering {
			return
		}
		switch node := node.(type) {
		case *syntax.CompareOpNode:
			if _, checked := types.TypeOf(node); checked && node.Kind.IsEqualityOp() {
				report(node, node.Left, node.Right)
			}
		case *syntax.FuncCallNode:
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

func identityConditionReference(expr syntax.Node) bool {
	switch syntax.StaticReference(expr) {
	case "github.ref", "github.ref_name", "github.head_ref", "github.base_ref",
		"github.actor", "github.triggering_actor", "github.event.sender.login",
		"github.event.deployment.environment", "github.event.label.name",
		"github.event.pull_request.head.ref", "github.event.pull_request.base.ref":
		return true
	}
	return false
}
