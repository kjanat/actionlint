package evaluate

import (
	"slices"
	"strings"

	exprfunctions "actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// SubexpressionResults records constant results for one syntax tree. Conditional
// branches can have local failures without making their parent fail. Branches
// proved unused are absent; unknown context values are not substituted.
type SubexpressionResults struct {
	root   exprsyntax.Node
	byNode map[exprsyntax.Node]exprvalues.EvaluationResult
}

// EvaluateSubexpressions records the root's demanded evaluation first. It then
// inspects branches whose execution depends on unknown values, following the
// shared function argument-demand contract. It does not evaluate skipped branches.
func EvaluateSubexpressions(root exprsyntax.Node) SubexpressionResults {
	results := make(map[exprsyntax.Node]exprvalues.EvaluationResult)
	expressionResult(root, nil, nil, results)
	skipped := make(map[exprsyntax.Node]bool)
	exprsyntax.Visit(root, func(node, parent exprsyntax.Node, entering bool) {
		if !entering || parent == nil {
			return
		}
		if skipped[parent] {
			skipped[node] = true
			return
		}
		if _, found := results[node]; found {
			return
		}
		if results[parent].State != exprvalues.EvaluationUnknown || argumentSkipped(parent, node, results) {
			skipped[node] = true
			return
		}
		expressionResult(node, nil, nil, results)
	})
	return SubexpressionResults{root: root, byNode: results}
}

func argumentSkipped(parent, node exprsyntax.Node, results map[exprsyntax.Node]exprvalues.EvaluationResult) bool {
	call, ok := parent.(*exprsyntax.FuncCallNode)
	if !ok {
		return false
	}
	function, found := exprfunctions.Builtins[strings.ToLower(call.Callee)]
	if !found || len(function.Signatures) == 0 {
		return false
	}
	args := make([]exprfunctions.CallArgument, len(call.Args))
	for index, child := range call.Args {
		arg := exprfunctions.CallArgument{Source: exprtypes.AnyType()}
		switch result := results[child]; result.State {
		case exprvalues.EvaluationKnown:
			arg.Source, arg.Value, arg.State = exprtypes.TypeOfJSONValue(result.Value), result.Value, exprfunctions.ArgumentKnown
		case exprvalues.EvaluationInvalid, exprvalues.EvaluationNotAttempted:
			arg.State = exprfunctions.ArgumentInvalid
		case exprvalues.EvaluationUnknown:
		}
		args[index] = arg
	}
	index := slices.Index(call.Args, node)
	return exprfunctions.DemandForArgument(function.Signatures[0].EvaluationPolicy, index, len(args), args) == exprfunctions.DemandSkipped
}

// Result returns a detached value and whether this node has a recorded result. A missing
// result means the branch was skipped or the node belongs to another tree.
func (results SubexpressionResults) Result(node exprsyntax.Node) (exprvalues.EvaluationResult, bool) {
	result, found := results.byNode[node]
	return result.Clone(), found
}

// Expression returns the syntax tree used to compute these results.
func (results SubexpressionResults) Expression() exprsyntax.Node { return results.root }
