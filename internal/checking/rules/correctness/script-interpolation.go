package correctness

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/evaluate"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/functions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/values"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	shellsyntax "mvdan.cc/sh/v3/syntax"
)

// ScriptInterpolationRule is the single definition of the script-interpolation rule.
var ScriptInterpolationRule = &define.Rule{
	Name:        "script-interpolation",
	Description: "Detect template values that may change script syntax",
	Category:    "correctness",
	Subcategory: "scripts",
	Explanation: "Detect template values that may change script syntax. Unknown strings are checked independently of attacker control. Constrained, nonempty literal strings can be used as shell words; empty and all-decimal strings need a proved data-argument position. Booleans, numeric values, optional matrix members, numeric identifiers, SHA values, and hashFiles results also require that position.",
	Help:        "Pass dynamic values through environment variables and read them as data in the script. Quoting a template expression does not prevent substitution before script parsing.",
	Recommended: true,
	Examples: []rule.Example{rule.LabeledWorkflowExample("Keep a dynamic value out of generated shell source.",
		"wrong: template value in script text", "better: template value in environment variable",
		"    steps:\n      - run: echo \"${{ vars.MESSAGE }}\"\n",
		"    steps:\n      - env: { MESSAGE: \"${{ vars.MESSAGE }}\" }\n        run: echo \"${MESSAGE}\"\n")},
	SupportsActions: true,
	DeclaredBy:      "expression",
}

// IsScalarInterpolation reports whether interpolation emits a boolean, number or null.
// toJSON serializes these primitives without adding string quotes.
func IsScalarInterpolation(node syntax.Node, types typecheck.InferredTypes) bool {
	if call, ok := node.(*syntax.FuncCallNode); ok && strings.EqualFold(call.Callee, "toJSON") && len(call.Args) == 1 {
		node = call.Args[0]
	}
	ty, known := types.TypeOf(node)
	if !known {
		return false
	}
	kind := ty.Kind()
	return kind == exprtypes.KindBool || kind == exprtypes.KindNumber || kind == exprtypes.KindNull
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

// ScriptInsertionKind identifies the syntax proved by the script's language and parser.
type ScriptInsertionKind uint8

const (
	UnknownScriptInsertion ScriptInsertionKind = iota
	ShellWordInsertion
	ShellArgumentInsertion
)

// ScriptInterpolation checks value characters and the script insertion context.
// Callers omit it when a specific secret or untrusted-input finding exists.
func ScriptInterpolation(expr syntax.Node, ty exprtypes.Type, types typecheck.InferredTypes, position ScriptInsertionKind, safeReference func(syntax.Node) bool) []*typecheck.Error {
	scalarArgument := position == ShellArgumentInsertion
	if ty.Kind() == exprtypes.KindString && IsScalarInterpolation(expr, types) {
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
	if position == UnknownScriptInsertion {
		return unsafeScriptInterpolation(expr)
	}
	if safeScriptExpression(expr, scalarArgument, safeReference) {
		return nil
	}
	return unsafeScriptInterpolation(expr)
}

func unsafeScalarScriptInterpolation(expr syntax.Node) []*typecheck.Error {
	return []*typecheck.Error{interpolationIssue(expr,
		"interpolated scalar is not proved to occupy a script data argument; pass it through an environment variable and read that variable as data instead")}
}

func unsafeScriptInterpolation(expr syntax.Node) []*typecheck.Error {
	return []*typecheck.Error{interpolationIssue(expr,
		"interpolated value may contain characters that change script syntax; pass it through an environment variable and read that variable as data instead. surrounding quotes do not prevent template substitution")}
}

func safeScriptExpression(expr syntax.Node, scalarArgument bool, safeReference func(syntax.Node) bool) bool {
	if value, known := evaluate.ConditionConstantValue(expr); known {
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
	switch strings.ToLower(syntax.DisplayReference(expr)) {
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
	case *syntax.CompareOpNode, *syntax.NotOpNode:
		return scalarArgument
	case *syntax.LogicalOpNode:
		if value, known := evaluate.ConditionConstantValue(n.Left); known {
			if values.ExpressionTruthy(value) != (n.Kind == syntax.LogicalOpNodeKindOr) {
				return safeScriptExpression(n.Right, scalarArgument, safeReference)
			}
			return safeScriptExpression(n.Left, scalarArgument, safeReference)
		}
		if n.Kind == syntax.LogicalOpNodeKindAnd {
			return safeFalsyScriptResult(n.Left, scalarArgument, safeReference) && safeScriptExpression(n.Right, scalarArgument, safeReference)
		}
		return safeTruthyScriptResult(n.Left, scalarArgument, safeReference) && safeScriptExpression(n.Right, scalarArgument, safeReference)
	case *syntax.FuncCallNode:
		if functions.IsStringPredicate(n.Callee) {
			return scalarArgument
		}
		if strings.EqualFold(n.Callee, "hashFiles") {
			return scalarArgument
		}
		if strings.EqualFold(n.Callee, "toJSON") && len(n.Args) == 1 {
			switch n.Args[0].(type) {
			case *syntax.CompareOpNode, *syntax.NotOpNode:
				return scalarArgument
			}
		}
		switch strings.ToLower(n.Callee) {
		case "success", "failure", "cancelled", "always":
			return scalarArgument
		}
		if strings.EqualFold(n.Callee, "case") && len(n.Args) >= 3 && len(n.Args)%2 == 1 {
			for i := 0; i < len(n.Args)-1; i += 2 {
				if value, known := evaluate.ConditionConstantValue(n.Args[i]); known {
					if !values.ExpressionTruthy(value) {
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

func safeTruthyScriptResult(expr syntax.Node, scalarArgument bool, safeReference func(syntax.Node) bool) bool {
	if value, known := evaluate.ConditionConstantValue(expr); known {
		if !values.ExpressionTruthy(value) {
			return true
		}
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	n, ok := expr.(*syntax.LogicalOpNode)
	if !ok {
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	switch n.Kind {
	case syntax.LogicalOpNodeKindAnd:
		return safeTruthyScriptResult(n.Right, scalarArgument, safeReference)
	case syntax.LogicalOpNodeKindOr:
		return safeTruthyScriptResult(n.Left, scalarArgument, safeReference) && safeTruthyScriptResult(n.Right, scalarArgument, safeReference)
	case syntax.LogicalOpNodeKindInvalid:
		// Invalid operators use the generic check below.
	}
	return safeScriptExpression(expr, scalarArgument, safeReference)
}

func safeFalsyScriptResult(expr syntax.Node, scalarArgument bool, safeReference func(syntax.Node) bool) bool {
	if scalarArgument {
		return true
	}
	if value, known := evaluate.ConditionConstantValue(expr); known {
		if values.ExpressionTruthy(value) {
			return true
		}
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	n, ok := expr.(*syntax.LogicalOpNode)
	if !ok {
		return safeScriptExpression(expr, scalarArgument, safeReference)
	}
	switch n.Kind {
	case syntax.LogicalOpNodeKindAnd:
		return safeFalsyScriptResult(n.Left, scalarArgument, safeReference) && safeFalsyScriptResult(n.Right, scalarArgument, safeReference)
	case syntax.LogicalOpNodeKindOr:
		return safeFalsyScriptResult(n.Right, scalarArgument, safeReference)
	case syntax.LogicalOpNodeKindInvalid:
		// Invalid operators use the generic check below.
	}
	return safeScriptExpression(expr, scalarArgument, safeReference)
}

// Step outcomes/conclusions and dependency results use fixed status enums.
func safeScriptStatusReference(node syntax.Node, scalarArgument bool, safeReference func(syntax.Node) bool) bool {
	var receiver syntax.Node
	var property string
	switch node := node.(type) {
	case *syntax.ObjectDerefNode:
		receiver, property = node.Receiver, node.Property
	case *syntax.IndexAccessNode:
		key, ok := node.Index.(*syntax.StringNode)
		if !ok {
			return false
		}
		receiver, property = node.Operand, key.Value
	default:
		return false
	}
	var context syntax.Node
	staticSelector := true
	switch receiver := receiver.(type) {
	case *syntax.ObjectDerefNode:
		context = receiver.Receiver
	case *syntax.IndexAccessNode:
		_, staticSelector = receiver.Index.(*syntax.StringNode)
		context = receiver.Operand
	default:
		return false
	}
	variable, ok := context.(*syntax.VariableNode)
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

func interpolationIssue(node syntax.Node, message string) *typecheck.Error {
	span := syntax.ExpressionSpan(node)
	return &typecheck.Error{Rule: "script-interpolation", Code: "unknown-script-value", Message: message, Offset: span.Start, EndOffset: span.End, Line: span.Line, Column: span.Column}
}
