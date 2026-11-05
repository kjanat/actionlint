package typecheck

import (
	"strings"

	expreval "actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"

	exprvalues "actionlint.kjanat.dev/v2/internal/languages/expressions/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	shellsyntax "mvdan.cc/sh/v3/syntax"
)

// IsScriptScalar resolves bounded scalar syntax without proving its script position safe.
func (sema *Checker) IsScriptScalar(node exprsyntax.Node) bool {
	if call, ok := node.(*exprsyntax.FuncCallNode); ok && strings.EqualFold(call.Callee, "toJSON") && len(call.Args) == 1 {
		// JSON adds no quoting to primitive non-string scalars. Do not recurse:
		// toJSON(toJSON(true)) serializes a string and introduces quote syntax.
		node = call.Args[0]
	}
	ty, known := sema.scriptReferenceType(node)
	if !known {
		return false
	}
	k := ty.Kind()
	return k == exprtypes.KindBool || k == exprtypes.KindNumber || k == exprtypes.KindNull
}

// scriptReferenceType types literals, status functions and static context
// references without reporting errors. known is false for anything else,
// including a property that a strict object does not declare.
func (sema *Checker) scriptReferenceType(node exprsyntax.Node) (ty exprtypes.Type, known bool) {
	property := func(receiver exprtypes.Type, name string) (exprtypes.Type, bool) {
		object, ok := receiver.Object()
		if !ok {
			return exprtypes.Type{}, false
		}
		if value, exists := object.Property(strings.ToLower(name)); exists {
			return value, true
		}
		return object.Mapped()
	}
	switch n := node.(type) {
	case *exprsyntax.BoolNode, *exprsyntax.CompareOpNode, *exprsyntax.NotOpNode:
		return exprtypes.BoolType(), true
	case *exprsyntax.IntNode, *exprsyntax.FloatNode:
		return exprtypes.NumberType(), true
	case *exprsyntax.NullNode:
		return exprtypes.NullType(), true
	case *exprsyntax.FuncCallNode:
		if isSafeFuncCall(n) {
			return exprtypes.BoolType(), true
		}
		switch strings.ToLower(n.Callee) {
		case "success", "failure", "cancelled", "always":
			return exprtypes.BoolType(), true
		}
	case *exprsyntax.VariableNode:
		ty, known := sema.vars[n.Name]
		return ty, known
	case *exprsyntax.ObjectDerefNode:
		receiver, known := sema.scriptReferenceType(n.Receiver)
		if !known {
			return exprtypes.Type{}, false
		}
		return property(receiver, n.Property)
	case *exprsyntax.IndexAccessNode:
		receiver, known := sema.scriptReferenceType(n.Operand)
		if !known {
			return exprtypes.Type{}, false
		}
		if array, ok := receiver.Array(); ok {
			return array.Elem(), true
		}
		if key, ok := n.Index.(*exprsyntax.StringNode); ok {
			return property(receiver, key.Value)
		}
		if object, ok := receiver.Object(); ok {
			return object.Mapped()
		}
	}
	return exprtypes.Type{}, false
}

// SafeScriptLiteral recognizes text without quoting, expansion, whitespace or
// command-separator characters. A proved value may be inserted without allowing
// its contents to introduce those constructs; it does not validate the command.
func SafeScriptLiteral(value string) bool {
	for _, r := range value {
		if scriptLiteralCharacter(r) {
			continue
		}
		return false
	}
	return true
}

func scriptLiteralCharacter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:-", r)
}

// SafeScriptArguments recognizes literal shell words separated by horizontal
// whitespace. It requires an independent proof that insertion occupies a whole
// command argument; it is not safe in command, assignment or redirection syntax.
func SafeScriptArguments(value string) bool {
	for _, r := range value {
		if r == ' ' || r == '\t' || scriptLiteralCharacter(r) {
			continue
		}
		return false
	}
	return true
}

// SafeScriptWord additionally excludes empty text, I/O numbers, and Bash/POSIX
// reserved words from unrestricted shell-word proofs.
func SafeScriptWord(value string) bool {
	// IsKeyword omits elif, which the parser handles as conditional syntax.
	return value != "" && SafeScriptLiteral(value) && !isScriptIONumber(value) && !shellsyntax.IsKeyword(value) && value != "elif"
}

// isScriptIONumber recognizes the unsigned decimal words that shells may parse
// as file descriptors when they directly precede a redirection operator.
func isScriptIONumber(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ScriptPosition identifies the syntax proved by the script's language and parser.
type ScriptPosition uint8

const (
	ScriptUnknownPosition ScriptPosition = iota
	ScriptShellWord
	ScriptShellArgument
)

// CheckScriptInterpolation needs separate character and scalar-argument position proofs.
// Callers omit it when a specific secret or untrusted-input finding exists.
func (sema *Checker) CheckScriptInterpolation(expr exprsyntax.Node, ty exprtypes.Type, position ScriptPosition, safeReference func(exprsyntax.Node) bool) []*Error {
	scalarArgument := position == ScriptShellArgument
	if ty.Kind() == exprtypes.KindString && sema.IsScriptScalar(expr) {
		if scalarArgument {
			return nil
		}
		return unsafeScalarScriptInterpolation(expr)
	}
	switch ty.Kind() {
	case exprtypes.KindObject, exprtypes.KindArray:
		// Template type validation already rejects these values in every position.
		return nil
	case exprtypes.KindBool, exprtypes.KindNumber, exprtypes.KindNull:
		if scalarArgument {
			return nil
		}
		return unsafeScalarScriptInterpolation(expr)
	case exprtypes.KindAny, exprtypes.KindString:
	}
	if position == ScriptUnknownPosition {
		return unsafeScriptInterpolation(expr)
	}
	if safeScriptExpression(expr, scalarArgument, safeReference) {
		return nil
	}
	return unsafeScriptInterpolation(expr)
}

func unsafeScalarScriptInterpolation(expr exprsyntax.Node) []*Error {
	return []*Error{errorfAtExpr(findingKind{"script-interpolation", "unknown-script-value"}, expr,
		"interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead")}
}

func unsafeScriptInterpolation(expr exprsyntax.Node) []*Error {
	return []*Error{errorfAtExpr(findingKind{"script-interpolation", "unknown-script-value"}, expr,
		"interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution")}
}

func safeScriptExpression(expr exprsyntax.Node, scalarArgument bool, safeReference func(exprsyntax.Node) bool) bool {
	if value, known := expreval.ConditionConstantValue(expr); known {
		switch value := value.(type) {
		case nil, bool, float64:
			return scalarArgument
		case string:
			return scalarArgument && SafeScriptLiteral(value) || SafeScriptWord(value)
		default:
			return false
		}
	}
	if safeReference != nil && safeReference(expr) {
		return true
	}
	if safeScriptStatusReference(expr, scalarArgument, safeReference) {
		return true
	}
	// Numeric identifiers and hashes need data-argument proofs. Actor names
	// may contain glob characters.
	switch strings.ToLower(eventReference(expr)) {
	case "github.repository_owner":
		return scalarArgument
	case "github.sha", "github.run_id", "github.run_number", "github.run_attempt",
		"github.repository_id", "github.repository_owner_id", "github.actor_id":
		return scalarArgument
	case "github.repository", "github.repositoryurl",
		"github.event_name", "github.ref_type", "github.repository_visibility",
		"runner.os", "runner.arch", "runner.environment", "job.status":
		return true
	}
	switch n := expr.(type) {
	case *exprsyntax.CompareOpNode, *exprsyntax.NotOpNode:
		return scalarArgument
	case *exprsyntax.LogicalOpNode:
		if value, known := expreval.ConditionConstantValue(n.Left); known {
			if exprvalues.ExpressionTruthy(value) != (n.Kind == exprsyntax.LogicalOpNodeKindOr) {
				return safeScriptExpression(n.Right, scalarArgument, safeReference)
			}
			return safeScriptExpression(n.Left, scalarArgument, safeReference)
		}
		if n.Kind == exprsyntax.LogicalOpNodeKindAnd {
			return safeFalsyScriptResult(n.Left, scalarArgument, safeReference) && safeScriptExpression(n.Right, scalarArgument, safeReference)
		}
		return safeTruthyScriptResult(n.Left, scalarArgument, safeReference) && safeScriptExpression(n.Right, scalarArgument, safeReference)
	case *exprsyntax.FuncCallNode:
		if isSafeFuncCall(n) {
			return scalarArgument
		}
		if strings.EqualFold(n.Callee, "hashFiles") {
			return scalarArgument
		}
		if strings.EqualFold(n.Callee, "toJSON") && len(n.Args) == 1 {
			switch n.Args[0].(type) {
			case *exprsyntax.CompareOpNode, *exprsyntax.NotOpNode:
				return scalarArgument
			}
		}
		switch strings.ToLower(n.Callee) {
		case "success", "failure", "cancelled", "always":
			return scalarArgument
		}
		if strings.EqualFold(n.Callee, "case") && len(n.Args) >= 3 && len(n.Args)%2 == 1 {
			for i := 0; i < len(n.Args)-1; i += 2 {
				if value, known := expreval.ConditionConstantValue(n.Args[i]); known {
					if !exprvalues.ExpressionTruthy(value) {
						continue
					}
					return safeScriptExpression(n.Args[i+1], scalarArgument, safeReference)
				}
				if !safeScriptExpression(n.Args[i+1], scalarArgument, safeReference) {
					return false
				}
			}
			return safeScriptExpression(n.Args[len(n.Args)-1], scalarArgument, safeReference)
		}
	}
	return false
}

func safeTruthyScriptResult(expr exprsyntax.Node, scalarArgument bool, safeReference func(exprsyntax.Node) bool) bool {
	if value, known := expreval.ConditionConstantValue(expr); known {
		if !exprvalues.ExpressionTruthy(value) {
			return true
		}
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	n, ok := expr.(*exprsyntax.LogicalOpNode)
	if !ok {
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	switch n.Kind {
	case exprsyntax.LogicalOpNodeKindAnd:
		return safeTruthyScriptResult(n.Right, scalarArgument, safeReference)
	case exprsyntax.LogicalOpNodeKindOr:
		return safeTruthyScriptResult(n.Left, scalarArgument, safeReference) && safeTruthyScriptResult(n.Right, scalarArgument, safeReference)
	case exprsyntax.LogicalOpNodeKindInvalid:
		// Invalid operators use the generic check below.
	}
	return safeScriptExpression(expr, scalarArgument, safeReference)
}

func safeFalsyScriptResult(expr exprsyntax.Node, scalarArgument bool, safeReference func(exprsyntax.Node) bool) bool {
	if scalarArgument {
		return true
	}
	if value, known := expreval.ConditionConstantValue(expr); known {
		if exprvalues.ExpressionTruthy(value) {
			return true
		}
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	n, ok := expr.(*exprsyntax.LogicalOpNode)
	if !ok {
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	switch n.Kind {
	case exprsyntax.LogicalOpNodeKindAnd:
		return safeFalsyScriptResult(n.Left, scalarArgument, safeReference) && safeFalsyScriptResult(n.Right, scalarArgument, safeReference)
	case exprsyntax.LogicalOpNodeKindOr:
		return safeFalsyScriptResult(n.Right, scalarArgument, safeReference)
	case exprsyntax.LogicalOpNodeKindInvalid:
		// Invalid operators use the generic check below.
	}
	return safeScriptExpression(expr, scalarArgument, safeReference)
}

// Step outcomes/conclusions and dependency results use fixed status enums.
func safeScriptStatusReference(node exprsyntax.Node, scalarArgument bool, safeReference func(exprsyntax.Node) bool) bool {
	var receiver exprsyntax.Node
	var property string
	switch node := node.(type) {
	case *exprsyntax.ObjectDerefNode:
		receiver, property = node.Receiver, node.Property
	case *exprsyntax.IndexAccessNode:
		key, ok := node.Index.(*exprsyntax.StringNode)
		if !ok {
			return false
		}
		receiver, property = node.Operand, key.Value
	default:
		return false
	}
	var context exprsyntax.Node
	staticSelector := true
	switch receiver := receiver.(type) {
	case *exprsyntax.ObjectDerefNode:
		context = receiver.Receiver
	case *exprsyntax.IndexAccessNode:
		_, staticSelector = receiver.Index.(*exprsyntax.StringNode)
		context = receiver.Operand
	default:
		return false
	}
	variable, ok := context.(*exprsyntax.VariableNode)
	if !ok {
		return false
	}
	switch strings.ToLower(variable.Name) {
	case "steps":
		if !strings.EqualFold(property, "conclusion") && !strings.EqualFold(property, "outcome") {
			return false
		}
	case "needs":
		if !strings.EqualFold(property, "result") {
			return false
		}
	default:
		return false
	}
	// A missing or dynamic selector can yield an empty string, which is safe
	// only when the script parser proved a data-argument position.
	return scalarArgument || staticSelector && safeReference != nil && safeReference(receiver)
}
