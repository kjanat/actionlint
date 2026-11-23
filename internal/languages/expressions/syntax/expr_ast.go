package syntax

import "actionlint.kjanat.dev/v2/internal/base/sourcetext"

// Node is a Node of expression syntax tree. To know the syntax, see
// https://docs.github.com/en/actions/learn-github-actions/expressions
type Node interface {
	Token() Token
}

// Span is a half-open byte range in the expression's decoded source.
// Position locates Start; a zero Position means the span is unknown.
type Span struct {
	Start, End int
	Position   sourcetext.Position
}

type Location struct {
	// First is the node's first token. Operators and dereferences repeat the
	// first token of their leftmost operand.
	First Token
	// Range covers the node in source, including enclosing parentheses.
	Range Span
}

func (l *Location) Token() Token { return l.First }

func (l *Location) Span() Span { return l.Range }

func (l *Location) location() *Location { return l }

func ExpressionSpan(node Node) Span {
	if n, ok := node.(interface{ Span() Span }); ok {
		if span := n.Span(); span.Position.Line != 0 {
			return span
		}
	}
	t := node.Token()
	return Span{t.Offset, t.Offset + len(t.Value), t.Position}
}

type VariableNode struct {
	Location
	// Name is name of the variable in lower case. First holds its spelling.
	Name string
}

type NullNode struct {
	Location
}

type BoolNode struct {
	Location
	Value bool
}

type IntNode struct {
	Location
	Value int
}

type FloatNode struct {
	Location
	Value float64
}

type StringNode struct {
	Location
	// Escapes are resolved and quotes at both edges are removed.
	Value string
}

type ObjectDerefNode struct {
	Location
	Receiver      Node
	Property      string
	PropertyToken Token
}

func (n *ObjectDerefNode) PropertyName() string {
	if n.PropertyToken.Value != "" {
		return n.PropertyToken.Value
	}
	return n.Property
}

type ArrayDerefNode struct {
	Location
	Receiver Node
}

type IndexAccessNode struct {
	Location
	Operand Node
	Index   Node
}

type NotOpNode struct {
	Location
	Operand Node
}

type CompareOpNodeKind int

const (
	CompareOpNodeKindInvalid CompareOpNodeKind = iota
	CompareOpNodeKindLess
	CompareOpNodeKindLessEq
	CompareOpNodeKindGreater
	CompareOpNodeKindGreaterEq
	CompareOpNodeKindEq
	CompareOpNodeKindNotEq
)

func (kind CompareOpNodeKind) IsEqualityOp() bool {
	return kind == CompareOpNodeKindEq || kind == CompareOpNodeKindNotEq
}

func (kind CompareOpNodeKind) String() string {
	switch kind {
	case CompareOpNodeKindLess:
		return "<"
	case CompareOpNodeKindLessEq:
		return "<="
	case CompareOpNodeKindGreater:
		return ">"
	case CompareOpNodeKindGreaterEq:
		return ">="
	case CompareOpNodeKindEq:
		return "=="
	case CompareOpNodeKindNotEq:
		return "!="
	case CompareOpNodeKindInvalid:
		fallthrough
	default:
		return ""
	}
}

type CompareOpNode struct {
	Location
	Kind  CompareOpNodeKind
	Left  Node
	Right Node
}

type LogicalOpNodeKind int

const (
	LogicalOpNodeKindInvalid LogicalOpNodeKind = iota
	LogicalOpNodeKindAnd
	LogicalOpNodeKindOr
)

func (k LogicalOpNodeKind) String() string {
	switch k {
	case LogicalOpNodeKindAnd:
		return "&&"
	case LogicalOpNodeKindOr:
		return "||"
	case LogicalOpNodeKindInvalid:
		fallthrough
	default:
		return "INVALID LOGICAL OPERATOR"
	}
}

type LogicalOpNode struct {
	Location
	Kind  LogicalOpNodeKind
	Left  Node
	Right Node
}

type FuncCallNode struct {
	Location
	Callee string
	Args   []Node
}

// VisitExprNodeFunc runs once before and once after a node's children, with
// entering true and false. parent is nil for the root.
type VisitExprNodeFunc func(node, parent Node, entering bool)

func visitExprNode(n, p Node, f VisitExprNodeFunc) {
	f(n, p, true)
	switch n := n.(type) {
	case *ObjectDerefNode:
		visitExprNode(n.Receiver, n, f)
	case *ArrayDerefNode:
		visitExprNode(n.Receiver, n, f)
	case *IndexAccessNode:
		// Index must be visited before Operand to make UntrustedInputChecker work correctly.
		visitExprNode(n.Index, n, f)
		visitExprNode(n.Operand, n, f)
	case *NotOpNode:
		visitExprNode(n.Operand, n, f)
	case *CompareOpNode:
		visitExprNode(n.Left, n, f)
		visitExprNode(n.Right, n, f)
	case *LogicalOpNode:
		visitExprNode(n.Left, n, f)
		visitExprNode(n.Right, n, f)
	case *FuncCallNode:
		for _, a := range n.Args {
			visitExprNode(a, n, f)
		}
	}
	f(n, p, false)
}

func Visit(n Node, f VisitExprNodeFunc) {
	visitExprNode(n, nil, f)
}
