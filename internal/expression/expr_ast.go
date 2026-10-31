package expression

// Node is a Node of expression syntax tree. To know the syntax, see
// https://docs.github.com/en/actions/learn-github-actions/expressions
//
// Nodes are plain syntax records, as in go/ast: the parser builds each node
// once and shares no storage with the lexer, so a tree can be read, copied or
// rewritten without affecting the parser that produced it.
type Node interface {
	// Token returns the first Token of the Node. This method is useful to get position of this Node.
	Token() Token
}

// Span is a half-open byte range in the expression's decoded source.
// Line and Column identify Start using one-based coordinates.
type Span struct {
	Start, End   int
	Line, Column int
}

// Location places a node in the expression it was parsed from. Every node
// embeds one. Nodes built outside the parser may leave it zero.
type Location struct {
	// First is the node's first token. Operators and dereferences repeat the
	// first token of their leftmost operand.
	First Token
	// Range covers the node in source, including enclosing parentheses. A
	// zero Line means the range is unknown.
	Range Span
}

// Token returns the first Token of the Node. This method is useful to get position of this Node.
func (l *Location) Token() Token { return l.First }

// Span returns the node's range in the parsed expression.
func (l *Location) Span() Span { return l.Range }

// location addresses the embedded Location of any node. Parentheses are only
// known to enclose a node after it is built, so the parser widens its Range.
func (l *Location) location() *Location { return l }

// ExpressionSpan returns the node's parsed range, or its first token's range
// when the node carries no range.
func ExpressionSpan(node Node) Span {
	if n, ok := node.(interface{ Span() Span }); ok {
		if span := n.Span(); span.Line != 0 {
			return span
		}
	}
	t := node.Token()
	return Span{t.Offset, t.Offset + len(t.Value), t.Line, t.Column}
}

// Variable

// VariableNode is Node for variable access.
type VariableNode struct {
	Location
	// Name is name of the variable in lower case. First holds its spelling.
	Name string
}

// Literals

// NullNode is Node for null literal.
type NullNode struct {
	Location
}

// BoolNode is Node for boolean literal, true or false.
type BoolNode struct {
	Location
	// Value is value of the boolean literal.
	Value bool
}

// IntNode is Node for integer literal.
type IntNode struct {
	Location
	// Value is value of the integer literal.
	Value int
}

// FloatNode is Node for float literal.
type FloatNode struct {
	Location
	// Value is value of the float literal.
	Value float64
}

// StringNode is Node for String literal.
type StringNode struct {
	Location
	// Value is value of the String literal. Escapes are resolved and quotes at both edges are
	// removed.
	Value string
}

// Operators

// ObjectDerefNode represents property dereference of object like 'foo.bar'.
type ObjectDerefNode struct {
	Location
	// Receiver is an expression at receiver of property dereference.
	Receiver Node
	// Property is a name of property to access in lower case.
	Property string
	// PropertyToken is the property's token. It is zero for nodes built
	// outside the parser.
	PropertyToken Token
}

// PropertyName returns the property's spelling in source.
func (n *ObjectDerefNode) PropertyName() string {
	if n.PropertyToken.Value != "" {
		return n.PropertyToken.Value
	}
	return n.Property
}

// ArrayDerefNode represents elements dereference of arrays like '*' in 'foo.bar.*.piyo'.
type ArrayDerefNode struct {
	Location
	// Receiver is an expression at receiver of array element dereference.
	Receiver Node
}

// IndexAccessNode is Node for index access, which represents dynamic object property access or
// array index access.
type IndexAccessNode struct {
	Location
	// Operand is an expression at operand of index access, which should be array or object.
	Operand Node
	// Index is an expression at index, which should be integer or String.
	Index Node
}

// Note: Currently only ! is a logical unary operator

// NotOpNode is Node for unary ! operator.
type NotOpNode struct {
	Location
	// Operand is an expression at operand of ! operator.
	Operand Node
}

// CompareOpNodeKind is a kind of compare operators; ==, !=, <, <=, >, >=.
type CompareOpNodeKind int

const (
	// CompareOpNodeKindInvalid is invalid and initial value of CompareOpNodeKind values.
	CompareOpNodeKindInvalid CompareOpNodeKind = iota
	// CompareOpNodeKindLess is kind for < operator.
	CompareOpNodeKindLess
	// CompareOpNodeKindLessEq is kind for <= operator.
	CompareOpNodeKindLessEq
	// CompareOpNodeKindGreater is kind for > operator.
	CompareOpNodeKindGreater
	// CompareOpNodeKindGreaterEq is kind for >= operator.
	CompareOpNodeKindGreaterEq
	// CompareOpNodeKindEq is kind for == operator.
	CompareOpNodeKindEq
	// CompareOpNodeKindNotEq is kind for != operator.
	CompareOpNodeKindNotEq
)

// IsEqualityOp returns true when it represents == or != operator.
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
		// Invalid operators have no spelling.
		fallthrough
	default:
		return ""
	}
}

// CompareOpNode is Node for binary expression to compare values; ==, !=, <, <=, > or >=.
type CompareOpNode struct {
	Location
	// Kind is a kind of this expression to show which operator is used.
	Kind CompareOpNodeKind
	// Left is an expression for left hand side of the binary operator.
	Left Node
	// Right is an expression for right hand side of the binary operator.
	Right Node
}

// LogicalOpNodeKind is a kind of logical operators; && and ||.
type LogicalOpNodeKind int

const (
	// LogicalOpNodeKindInvalid is an invalid and initial value of LogicalOpNodeKind.
	LogicalOpNodeKindInvalid LogicalOpNodeKind = iota
	// LogicalOpNodeKindAnd is a kind for && operator.
	LogicalOpNodeKindAnd
	// LogicalOpNodeKindOr is a kind for || operator.
	LogicalOpNodeKindOr
)

func (k LogicalOpNodeKind) String() string {
	switch k {
	case LogicalOpNodeKindAnd:
		return "&&"
	case LogicalOpNodeKindOr:
		return "||"
	case LogicalOpNodeKindInvalid:
		// Invalid operators render as a placeholder.
		fallthrough
	default:
		return "INVALID LOGICAL OPERATOR"
	}
}

// LogicalOpNode is Node for logical binary operators; && or ||.
type LogicalOpNode struct {
	Location
	// Kind is a kind to show which operator is used.
	Kind LogicalOpNodeKind
	// Left is an expression for left hand side of the binary operator.
	Left Node
	// Right is an expression for right hand side of the binary operator.
	Right Node
}

// FuncCallNode represents function call in expression.
// Note that currently only calling builtin functions is supported.
type FuncCallNode struct {
	Location
	// Callee is a name of called function. This is String value because currently only built-in
	// functions can be called. First holds the callee token.
	Callee string
	// Args is arguments of the function call.
	Args []Node
}

// VisitExprNodeFunc is a visitor function for Visit(). The entering argument is set to
// true when it is called before visiting children. It is set to false when it is called after
// visiting children. It means that this function is called twice for the same Node. The parent
// argument is the parent of the Node. When the Node is root, its parent is nil.
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

// Visit visits the given expression syntax tree with given function f.
func Visit(n Node, f VisitExprNodeFunc) {
	visitExprNode(n, nil, f)
}
