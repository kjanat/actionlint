package conditions

import (
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

func Condition(condition value.Value) (enabled, known bool) {
	return invocationConditionInStatuses(condition, []string{"success", "failure", "other"})
}

func JobCondition(condition value.Value, hasNeeds bool) (enabled, known bool) {
	if !hasNeeds {
		// No ancestor can have failed. Cancellation still prevents assuming success.
		return invocationConditionInStatuses(condition, []string{"success", "other"})
	}
	return Condition(condition)
}

func invocationConditionInStatuses(condition value.Value, statuses []string) (enabled, known bool) {
	if condition.State() == value.Dynamic {
		expr, parseErr, _ := exprsyntax.ParseConditionExpression(condition.Source())
		if parseErr != nil {
			return false, false
		}
		if conditionNeverRunsInStatuses(expr, statuses) {
			return false, true
		}
	}
	return StepCondition(condition)
}

type conditionTruth uint8

const (
	conditionUnknown conditionTruth = iota
	conditionFalse
	conditionTrue
)

// NeverRuns proves a job or step condition false in every possible status.
// For jobs, success requires a successful dependency chain, excluding failure in
// that chain. For steps, success() and failure() inspect the same execution status.
// cancelled() inspects the job even inside composites, whose other two status
// functions inspect action_status instead.
// Keep cancellation independent and leave unsupported expressions unknown.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idneeds
func NeverRuns(expression exprsyntax.Node) bool {
	return conditionNeverRunsInStatuses(expression, []string{"success", "failure", "other"})
}

// AlwaysIgnoresCancellation reports an explicit always() in a condition that
// cannot be proved false during cancellation. Unknown context values stay
// unknown; a guard such as !cancelled() must hold on every possible true path.
func AlwaysIgnoresCancellation(expression exprsyntax.Node) bool {
	hasAlways := false
	exprsyntax.Visit(expression, func(node, _ exprsyntax.Node, entering bool) {
		if call, ok := node.(*exprsyntax.FuncCallNode); entering && ok && len(call.Args) == 0 && strings.EqualFold(call.Callee, "always") {
			hasAlways = true
		}
	})
	if !hasAlways {
		return false
	}
	return conditionMayRunCancelled(expression)
}

// CanRunDuringCancellation checks explicit status functions. Without one, GitHub
// adds success() and cancellation stops the job. Context values stay unknown.
func CanRunDuringCancellation(expression exprsyntax.Node) bool {
	hasStatus := false
	exprsyntax.Visit(expression, func(node, _ exprsyntax.Node, entering bool) {
		if call, ok := node.(*exprsyntax.FuncCallNode); entering && ok && len(call.Args) == 0 {
			switch strings.ToLower(call.Callee) {
			case "always", "success", "failure", "cancelled":
				hasStatus = true
			}
		}
	})
	return hasStatus && conditionMayRunCancelled(expression)
}

func conditionMayRunCancelled(expression exprsyntax.Node) bool {
	for _, status := range []string{"success", "failure", "other"} {
		if conditionStatusTruth(expression, status, true) != conditionFalse {
			return true
		}
	}
	return false
}

func conditionNeverRunsInStatuses(expression exprsyntax.Node, statuses []string) bool {
	for _, status := range statuses {
		for _, cancelled := range []bool{false, true} {
			if conditionStatusTruth(expression, status, cancelled) != conditionFalse {
				return false
			}
		}
	}
	return true
}

func conditionStatusTruth(expression exprsyntax.Node, status string, cancelled bool) conditionTruth {
	return conditionContextTruth(expression, status, cancelled, nil)
}

// NeverRunsWithVariables proves a condition false for supplied context values
// across execution statuses. Unsupported values and operations stay unknown.
func NeverRunsWithVariables(expression exprsyntax.Node, variables func(string) (any, bool)) bool {
	for _, status := range []string{"success", "failure", "other"} {
		for _, cancelled := range []bool{false, true} {
			if conditionContextTruth(expression, status, cancelled, variables) != conditionFalse {
				return false
			}
		}
	}
	return true
}

func conditionContextTruth(expression exprsyntax.Node, status string, cancelled bool, variables func(string) (any, bool)) conditionTruth {
	if result, known := expreval.ExpressionValueWithLookup(expression, variables); known {
		return knownConditionTruth(exprvalues.ExpressionTruthy(result))
	}
	switch node := expression.(type) {
	case *exprsyntax.NullNode:
		return conditionFalse
	case *exprsyntax.BoolNode:
		return knownConditionTruth(node.Value)
	case *exprsyntax.IntNode:
		return knownConditionTruth(node.Value != 0)
	case *exprsyntax.FloatNode:
		return knownConditionTruth(node.Value != 0)
	case *exprsyntax.StringNode:
		return knownConditionTruth(node.Value != "")
	case *exprsyntax.FuncCallNode:
		if len(node.Args) != 0 {
			return conditionUnknown
		}
		switch strings.ToLower(node.Callee) {
		case "always":
			return conditionTrue
		case "success", "failure":
			return knownConditionTruth(strings.EqualFold(node.Callee, status))
		case "cancelled":
			return knownConditionTruth(cancelled)
		}
	case *exprsyntax.NotOpNode:
		switch conditionContextTruth(node.Operand, status, cancelled, variables) {
		case conditionFalse:
			return conditionTrue
		case conditionTrue:
			return conditionFalse
		case conditionUnknown:
			return conditionUnknown
		}
	case *exprsyntax.LogicalOpNode:
		left := conditionContextTruth(node.Left, status, cancelled, variables)
		right := conditionContextTruth(node.Right, status, cancelled, variables)
		switch node.Kind {
		case exprsyntax.LogicalOpNodeKindAnd:
			if left == conditionFalse || right == conditionFalse {
				return conditionFalse
			}
			if left == conditionTrue && right == conditionTrue {
				return conditionTrue
			}
		case exprsyntax.LogicalOpNodeKindOr:
			if left == conditionTrue || right == conditionTrue {
				return conditionTrue
			}
			if left == conditionFalse && right == conditionFalse {
				return conditionFalse
			}
		case exprsyntax.LogicalOpNodeKindInvalid:
		}
	case *exprsyntax.CompareOpNode:
		if result, known := conditionContextValue(node, status, cancelled, variables); known {
			return knownConditionTruth(exprvalues.ExpressionTruthy(result))
		}
	}
	return conditionUnknown
}

// Comparisons use operand values: && and || return an operand, rather
// than converting it to bool. In particular, always() && 'x' is not true == 1.
func conditionContextValue(expression exprsyntax.Node, status string, cancelled bool, variables func(string) (any, bool)) (any, bool) {
	if constant, known := expreval.ExpressionValueWithLookup(expression, variables); known {
		return constant, true
	}
	switch node := expression.(type) {
	case *exprsyntax.FuncCallNode, *exprsyntax.NotOpNode:
		truth := conditionContextTruth(expression, status, cancelled, variables)
		return truth == conditionTrue, truth != conditionUnknown
	case *exprsyntax.LogicalOpNode:
		left, known := conditionContextValue(node.Left, status, cancelled, variables)
		if !known {
			return nil, false
		}
		switch node.Kind {
		case exprsyntax.LogicalOpNodeKindAnd:
			if !exprvalues.ExpressionTruthy(left) {
				return left, true
			}
		case exprsyntax.LogicalOpNodeKindOr:
			if exprvalues.ExpressionTruthy(left) {
				return left, true
			}
		case exprsyntax.LogicalOpNodeKindInvalid:
			return nil, false
		}
		return conditionContextValue(node.Right, status, cancelled, variables)
	case *exprsyntax.CompareOpNode:
		left, leftKnown := conditionContextValue(node.Left, status, cancelled, variables)
		right, rightKnown := conditionContextValue(node.Right, status, cancelled, variables)
		if leftKnown && rightKnown {
			return exprvalues.CompareConditionValues(node.Kind, left, right)
		}
	}
	return nil, false
}

func knownConditionTruth(value bool) conditionTruth {
	if value {
		return conditionTrue
	}
	return conditionFalse
}

func StepCondition(condition value.Value) (enabled, known bool) {
	if condition.State() == value.Missing {
		return true, true
	}
	if condition.Known() {
		return SemanticTruth(condition)
	}
	if condition.State() != value.Dynamic {
		return false, false
	}
	expression, parseErr, _ := exprsyntax.ParseConditionExpression(condition.Source())
	if parseErr != nil {
		return false, false
	}
	constant, known := expreval.ConditionConstantValue(expression)
	if !known {
		// Explicit success() has the same gate as an ordinary step.
		if call, ok := expression.(*exprsyntax.FuncCallNode); ok && strings.EqualFold(call.Callee, "success") && len(call.Args) == 0 {
			return true, true
		}
		return false, false
	}
	switch value := constant.(type) {
	case nil:
		return false, true
	case bool:
		return value, true
	case float64:
		return value != 0, true
	case string:
		return value != "", true
	default:
		// Known JSON arrays and objects are truthy, including empty ones.
		return true, true
	}
}

func SemanticTruth(semantic value.Value) (enabled, known bool) {
	if !semantic.Known() {
		return false, false
	}
	switch semantic.Kind() {
	case value.Null:
		return false, true
	case value.Bool:
		result, _ := semantic.Bool()
		return result, true
	case value.Number:
		result, _ := semantic.Number()
		return result != 0, true
	case value.String:
		result, _ := semantic.Text()
		return result != "", true
	case value.Array, value.Object:
		return true, true
	case value.KindUnknown:
	}
	return false, false
}

func CanRunAfterFailure(condition value.Value) bool {
	if condition.State() == value.Missing {
		return false
	}
	if condition.State() == value.Invalid {
		return true
	}
	if condition.State() != value.Dynamic {
		return false
	}
	expression, err, _ := exprsyntax.ParseConditionExpression(condition.Source())
	if err != nil || expression == nil {
		return true
	}
	hasStatusFunction := false
	exprsyntax.Visit(expression, func(node, _ exprsyntax.Node, entering bool) {
		if call, ok := node.(*exprsyntax.FuncCallNode); entering && ok {
			switch strings.ToLower(call.Callee) {
			case "always", "cancelled", "failure", "success":
				hasStatusFunction = true
			}
		}
	})
	// A status function removes the runner's implicit success() condition.
	return hasStatusFunction && !expressionRequiresSuccess(expression)
}

func expressionRequiresSuccess(expression exprsyntax.Node) bool {
	switch node := expression.(type) {
	case *exprsyntax.FuncCallNode:
		return strings.EqualFold(node.Callee, "success") && len(node.Args) == 0
	case *exprsyntax.LogicalOpNode:
		left, right := expressionRequiresSuccess(node.Left), expressionRequiresSuccess(node.Right)
		switch node.Kind {
		case exprsyntax.LogicalOpNodeKindAnd:
			return left || right
		case exprsyntax.LogicalOpNodeKindOr:
			return left && right
		case exprsyntax.LogicalOpNodeKindInvalid:
		}
		return false
	default:
		return false
	}
}

func BoolMayBeTrue(semantic value.Value) bool {
	if semantic.State() == value.Missing {
		return false
	}
	if enabled, known := semantic.Bool(); known {
		return enabled
	}
	return true
}
