package evaluate

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprvalues"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

type SubexpressionResults struct {
	root   syntax.Node
	byNode map[syntax.Node]exprvalues.EvaluationResult
}

func EvaluateSubexpressions(root syntax.Node) SubexpressionResults {
	results := make(map[syntax.Node]exprvalues.EvaluationResult)
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
		if results[parent].State != exprvalues.EvaluationUnknown || argumentSkipped(parent, node, results) {
			skipped[node] = true
			return
		}
		expressionResult(node, nil, nil, results)
	})
	return SubexpressionResults{root: root, byNode: results}
}

func argumentSkipped(parent, node syntax.Node, results map[syntax.Node]exprvalues.EvaluationResult) bool {
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
		case exprvalues.EvaluationKnown:
			arg.Source, arg.Value, arg.State = exprtypes.TypeOfJSONValue(result.Value), result.Value, functions.ArgumentKnown
		case exprvalues.EvaluationInvalid, exprvalues.EvaluationNotAttempted:
			arg.State = functions.ArgumentInvalid
		case exprvalues.EvaluationUnknown:
		}
		args[index] = arg
	}
	index := slices.Index(call.Args, node)
	return functions.DemandForArgument(function.Signatures[0].EvaluationPolicy, index, len(args), args) == functions.DemandSkipped
}

// Result reports false for a skipped branch or a node from another tree.
func (results SubexpressionResults) Result(node syntax.Node) (exprvalues.EvaluationResult, bool) {
	result, found := results.byNode[node]
	return result.Clone(), found
}

func (results SubexpressionResults) Expression() syntax.Node { return results.root }
