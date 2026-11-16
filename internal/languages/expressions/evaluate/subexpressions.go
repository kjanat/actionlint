package evaluate

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
)

// SubexpressionResults records constant results for one syntax tree. Conditional
// branches can have local failures without making their parent fail. Branches
// proved unused are absent; unknown context values are not substituted.
type SubexpressionResults struct {
	root   syntax.Node
	byNode map[syntax.Node]values.EvaluationResult
}

// EvaluateSubexpressions records the root's demanded evaluation first. It then
// inspects branches whose execution depends on unknown values, following the
// shared function argument-demand contract. It does not evaluate skipped branches.
func EvaluateSubexpressions(root syntax.Node) SubexpressionResults {
	results := make(map[syntax.Node]values.EvaluationResult)
	expressionResult(root, nil, nil, results)
	skipped := make(map[syntax.Node]bool)
	syntax.Visit(root, func(node, parent syntax.Node, entering bool) {
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
		if results[parent].State != values.EvaluationUnknown || argumentSkipped(parent, node, results) {
			skipped[node] = true
			return
		}
		expressionResult(node, nil, nil, results)
	})
	return SubexpressionResults{root: root, byNode: results}
}

func argumentSkipped(parent, node syntax.Node, results map[syntax.Node]values.EvaluationResult) bool {
	call, ok := parent.(*syntax.FuncCallNode)
	if !ok {
		return false
	}
	function, found := functions.Builtins[strings.ToLower(call.Callee)]
	if !found || len(function.Signatures) == 0 {
		return false
	}
	args := make([]functions.CallArgument, len(call.Args))
	for index, child := range call.Args {
		arg := functions.CallArgument{Source: exprtypes.AnyType()}
		switch result := results[child]; result.State {
		case values.EvaluationKnown:
			arg.Source, arg.Value, arg.State = exprtypes.TypeOfJSONValue(result.Value), result.Value, functions.ArgumentKnown
		case values.EvaluationInvalid, values.EvaluationNotAttempted:
			arg.State = functions.ArgumentInvalid
		case values.EvaluationUnknown:
		}
		args[index] = arg
	}
	index := slices.Index(call.Args, node)
	return functions.DemandForArgument(function.Signatures[0].EvaluationPolicy, index, len(args), args) == functions.DemandSkipped
}

// Result returns a detached value and whether this node has a recorded result. A missing
// result means the branch was skipped or the node belongs to another tree.
func (results SubexpressionResults) Result(node syntax.Node) (values.EvaluationResult, bool) {
	result, found := results.byNode[node]
	return result.Clone(), found
}

// Expression returns the syntax tree used to compute these results.
func (results SubexpressionResults) Expression() syntax.Node { return results.root }
