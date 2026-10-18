package expression

import "strings"

// IsScriptSafeScalar resolves reference types without rerunning semantic checks
// or changing diagnostics. Boolean, number and null values have bounded syntax.
func (sema *Checker) IsScriptSafeScalar(node Node) bool {
	if call, ok := node.(*FuncCallNode); ok && strings.EqualFold(call.Callee, "toJSON") && len(call.Args) == 1 {
		// JSON adds no quoting to primitive non-string scalars. Do not recurse:
		// toJSON(toJSON(true)) serializes a string and introduces quote syntax.
		node = call.Args[0]
	}
	switch sema.scriptReferenceType(node).(type) {
	case BoolType, NumberType, NullType:
		return true
	default:
		return false
	}
}

func (sema *Checker) scriptReferenceType(node Node) Type {
	property := func(receiver Type, name string) Type {
		if object, ok := receiver.(*ObjectType); ok {
			if value, exists := object.Props[strings.ToLower(name)]; exists {
				return value
			}
			return object.Mapped
		}
		return nil
	}
	switch n := node.(type) {
	case *BoolNode, *CompareOpNode, *NotOpNode:
		return BoolType{}
	case *IntNode, *FloatNode:
		return NumberType{}
	case *NullNode:
		return NullType{}
	case *FuncCallNode:
		if isSafeFuncCall(n) {
			return BoolType{}
		}
		switch strings.ToLower(n.Callee) {
		case "success", "failure", "cancelled", "always":
			return BoolType{}
		}
	case *VariableNode:
		return sema.vars[n.Name]
	case *ObjectDerefNode:
		return property(sema.scriptReferenceType(n.Receiver), n.Property)
	case *IndexAccessNode:
		receiver := sema.scriptReferenceType(n.Operand)
		if array, ok := receiver.(*ArrayType); ok {
			return array.Elem
		}
		if key, ok := n.Index.(*StringNode); ok {
			return property(receiver, key.Value)
		}
		if object, ok := receiver.(*ObjectType); ok {
			return object.Mapped
		}
	}
	return nil
}

// SafeScriptLiteral recognizes text without quoting, expansion, whitespace or
// command-separator characters. A proved value may be inserted without allowing
// its contents to introduce those constructs; it does not validate the command.
func SafeScriptLiteral(value string) bool {
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:-", r) {
			continue
		}
		return false
	}
	return true
}

// CheckScriptInterpolation checks direct template values independently of
// attacker control. safeReference proves every possible value of a reference
// has safe characters, for example a static matrix axis. Callers should retain
// more specific secret and untrusted-input findings instead of duplicating them.
func CheckScriptInterpolation(expr Node, ty Type, safeReference func(Node) bool) []*Error {
	switch ty.(type) {
	case BoolType, NumberType, NullType, *ObjectType, *ArrayType:
		return nil
	}
	if safeScriptExpression(expr, safeReference) {
		return nil
	}
	return []*Error{errorfAtExpr(findingKind{"script-interpolation", "unknown-script-value"}, expr,
		"interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution")}
}

func safeScriptExpression(expr Node, safeReference func(Node) bool) bool {
	if value, known := ConditionConstantValue(expr); known {
		switch value := value.(type) {
		case nil, bool, float64:
			return true
		case string:
			return SafeScriptLiteral(value)
		default:
			return false
		}
	}
	if safeReference != nil && safeReference(expr) {
		return true
	}
	// These contexts have fixed identifier, numeric, hash or enum domains in
	// GitHub's context contract. Branch names, workflow names and user outputs
	// remain arbitrary strings. Actor names are not included: bot logins can
	// contain brackets, which have shell glob syntax.
	switch strings.ToLower(eventReference(expr)) {
	case "github.sha", "github.run_id", "github.run_number", "github.run_attempt",
		"github.repository_id", "github.repository_owner_id", "github.actor_id",
		"github.repository", "github.repository_owner", "github.repositoryurl",
		"github.event_name", "github.ref_type", "github.repository_visibility",
		"runner.os", "runner.arch", "runner.environment", "job.status":
		return true
	}
	switch n := expr.(type) {
	case *CompareOpNode, *NotOpNode:
		return true
	case *LogicalOpNode:
		if value, known := ConditionConstantValue(n.Left); known {
			if ExpressionTruthy(value) != (n.Kind == LogicalOpNodeKindOr) {
				return safeScriptExpression(n.Right, safeReference)
			}
			return safeScriptExpression(n.Left, safeReference)
		}
		return (n.Kind == LogicalOpNodeKindAnd || safeScriptExpression(n.Left, safeReference)) && safeScriptExpression(n.Right, safeReference)
	case *FuncCallNode:
		if isSafeFuncCall(n) || strings.EqualFold(n.Callee, "hashFiles") {
			return true
		}
		if strings.EqualFold(n.Callee, "toJSON") && len(n.Args) == 1 {
			switch n.Args[0].(type) {
			case *CompareOpNode, *NotOpNode:
				return true
			}
		}
		switch strings.ToLower(n.Callee) {
		case "success", "failure", "cancelled", "always":
			return true
		}
		if strings.EqualFold(n.Callee, "case") && len(n.Args) >= 3 && len(n.Args)%2 == 1 {
			for i := 0; i < len(n.Args)-1; i += 2 {
				if value, known := ConditionConstantValue(n.Args[i]); known {
					if !ExpressionTruthy(value) {
						continue
					}
					return safeScriptExpression(n.Args[i+1], safeReference)
				}
				if !safeScriptExpression(n.Args[i+1], safeReference) {
					return false
				}
			}
			return safeScriptExpression(n.Args[len(n.Args)-1], safeReference)
		}
	}
	return false
}
