package expression

import (
	"fmt"
	"strconv"
	"strings"

	stringutil "actionlint.kjanat.dev/v2/internal/stringutil"
)

func errorAtToken(kind findingKind, t *Token, msg string) *Error {
	return &Error{
		Message: msg,
		Rule:    kind.rule, Code: kind.code,
		Offset:    t.Offset,
		EndOffset: t.Offset + len(t.Value),
		Line:      t.Line,
		Column:    t.Column,
	}
}

// Parser is a Parser for expression syntax. To know the details, see
// https://docs.github.com/en/actions/learn-github-actions/expressions
type Parser struct {
	cur   *Token
	lexer *Lexer
	err   *Error
	last  *Token
}

// NewParser creates new Parser instance.
func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) error(msg string) {
	if p.err == nil {
		p.err = errorAtToken(syntax, p.cur, msg)
	}
}

func (p *Parser) errorf(format string, args ...any) {
	p.error(fmt.Sprintf(format, args...))
}

func (p *Parser) unexpected(where string, expected []TokenKind) {
	if p.err != nil {
		return
	}
	qb := stringutil.QuotesBuilder{}
	for _, e := range expected {
		qb.Append(e.String())
	}
	var what string
	if p.cur.Kind == TokenKindEnd {
		what = "end of input"
	} else {
		what = fmt.Sprintf("token %q", p.cur.Kind.String())
	}
	msg := fmt.Sprintf("unexpected %s while parsing %s. expecting %s", what, where, qb.Build())
	p.error(msg)
}

func (p *Parser) next() *Token {
	ret := p.cur
	p.last = ret
	p.cur = p.lexer.Next()
	return ret
}

type parsedExprNode interface {
	Node
	setSpan(Span)
}

func (p *Parser) node(n parsedExprNode, start *Token) parsedExprNode {
	n.setSpan(Span{start.Offset, p.last.Offset + len(p.last.Value), start.Line, start.Column})
	return n
}

func (p *Parser) peek() *Token {
	return p.cur
}

func (p *Parser) parseIdent() parsedExprNode {
	ident := p.next() // eat ident
	if p.peek().Kind == TokenKindLeftParen {
		// Parse function call as primary expression though generally function call is parsed as
		// postfix expression. The reason is that only built-in function call is allowed in workflow
		// expression syntax, meant that callee is always built-in function name, not a general
		// expression.
		p.next() // eat '('
		args := []Node{}
		if p.peek().Kind == TokenKindRightParen {
			// no arguments
			p.next() // eat ')'
		} else {
		LoopArgs:
			for {
				arg := p.parseLogicalOr()
				if arg == nil {
					return nil
				}

				args = append(args, arg)

				if p.peek().Kind == TokenKindRightParen {
					p.next() // eat ')'
					break LoopArgs
				}
				if p.peek().Kind != TokenKindComma {
					p.unexpected("arguments of function call", []TokenKind{TokenKindComma, TokenKindRightParen})
					return nil
				}
				p.next() // eat ','
				// continue to next argument
			}
		}
		return p.node(&FuncCallNode{Callee: ident.Value, Args: args, tok: ident}, ident)
	}
	// Handle keywords. Note that keywords are case sensitive. TRUE, FALSE, NULL are invalid named value.
	switch ident.Value {
	case "null":
		return p.node(&NullNode{tok: ident}, ident)
	case "true":
		return p.node(&BoolNode{Value: true, tok: ident}, ident)
	case "false":
		return p.node(&BoolNode{Value: false, tok: ident}, ident)
	default:
		// Variable name access is case insensitive. github.event and GITHUB.event are the same.
		return p.node(&VariableNode{Name: strings.ToLower(ident.Value), tok: ident}, ident)
	}
}

func (p *Parser) parseNestedExpr() parsedExprNode {
	start := p.next() // eat '('

	nested := p.parseLogicalOr()
	if nested == nil {
		return nil
	}

	if p.peek().Kind == TokenKindRightParen {
		p.next() // eat ')'
	} else {
		p.unexpected("closing ')' of nested expression (...)", []TokenKind{TokenKindRightParen})
		return nil
	}

	return p.node(nested, start)
}

func (p *Parser) parseInt() parsedExprNode {
	t := p.peek()
	i, err := strconv.ParseInt(t.Value, 0, 32)
	if err != nil {
		p.errorf("parsing invalid integer literal %q: %s", t.Value, err)
		return nil
	}

	p.next() // eat int

	return p.node(&IntNode{Value: int(i), tok: t}, t)
}

func (p *Parser) parseFloat() parsedExprNode {
	t := p.peek()
	f, err := strconv.ParseFloat(t.Value, 64)
	if err != nil {
		p.errorf("parsing invalid float literal %q: %s", t.Value, err)
		return nil
	}

	p.next() // eat float

	return p.node(&FloatNode{Value: f, tok: t}, t)
}

func (p *Parser) parseString() parsedExprNode {
	t := p.next() // eat string
	s := t.Value
	s = s[1 : len(s)-1]                  // strip first and last single quotes
	s = strings.ReplaceAll(s, "''", "'") // unescape ''
	return p.node(&StringNode{Value: s, tok: t}, t)
}

func (p *Parser) parsePrimaryExpr() parsedExprNode {
	kind := p.peek().Kind
	if kind == TokenKindIdent {
		return p.parseIdent()
	}
	if kind == TokenKindLeftParen {
		return p.parseNestedExpr()
	}
	if kind == TokenKindInt {
		return p.parseInt()
	}
	if kind == TokenKindFloat {
		return p.parseFloat()
	}
	if kind == TokenKindString {
		return p.parseString()
	}
	p.unexpected(
		"variable access, function call, null, bool, int, float or string",
		[]TokenKind{
			TokenKindIdent,
			TokenKindLeftParen,
			TokenKindInt,
			TokenKindFloat,
			TokenKindString,
		},
	)
	return nil
}

func (p *Parser) parsePostfixOp() parsedExprNode {
	start := p.peek()
	ret := p.parsePrimaryExpr()
	if ret == nil {
		return nil
	}

	for {
		kind := p.peek().Kind
		if kind == TokenKindDot {
			p.next() // eat '.'
			ret = p.parseDeref(ret, start)
			if ret == nil {
				return nil
			}
			continue
		}
		if kind != TokenKindLeftBracket {
			return ret
		}
		p.next() // eat '['
		idx := p.parseLogicalOr()
		if idx == nil {
			return nil
		}
		if p.peek().Kind != TokenKindRightBracket {
			p.unexpected("closing bracket ']' for index access", []TokenKind{TokenKindRightBracket})
			return nil
		}
		p.next() // eat ']'
		ret = p.node(&IndexAccessNode{Operand: ret, Index: idx}, start)
	}
}

// parseDeref parses the property or '*' following '.' in 'a.b' or 'a.*'.
func (p *Parser) parseDeref(receiver Node, start *Token) parsedExprNode {
	if p.peek().Kind == TokenKindStar {
		p.next() // eat '*'
		return p.node(&ArrayDerefNode{Receiver: receiver}, start)
	}
	if p.peek().Kind == TokenKindIdent {
		t := p.next() // eat 'b' of 'a.b'
		// Property name is case insensitive. github.event and github.EVENT are the same
		return p.node(&ObjectDerefNode{Receiver: receiver, Property: strings.ToLower(t.Value), propertyToken: t}, start)
	}
	p.unexpected(
		"object property dereference like 'a.b' or array element dereference like 'a.*'",
		[]TokenKind{TokenKindIdent, TokenKindStar},
	)
	return nil
}

func (p *Parser) parsePrefixOp() parsedExprNode {
	t := p.peek()
	if t.Kind != TokenKindNot {
		return p.parsePostfixOp()
	}
	p.next() // eat '!' token

	o := p.parsePrefixOp()
	if o == nil {
		return nil
	}

	return p.node(&NotOpNode{Operand: o, tok: t}, t)
}

// compareOpTokens maps comparison operator tokens to their node kinds.
var compareOpTokens = map[TokenKind]CompareOpNodeKind{
	TokenKindLess:      CompareOpNodeKindLess,
	TokenKindLessEq:    CompareOpNodeKindLessEq,
	TokenKindGreater:   CompareOpNodeKindGreater,
	TokenKindGreaterEq: CompareOpNodeKindGreaterEq,
	TokenKindEq:        CompareOpNodeKindEq,
	TokenKindNotEq:     CompareOpNodeKindNotEq,
}

func (p *Parser) parseCompareBinOp() parsedExprNode {
	start := p.peek()
	l := p.parsePrefixOp()
	if l == nil {
		return nil
	}

	k, ok := compareOpTokens[p.peek().Kind]
	if !ok {
		return l
	}
	p.next() // eat the operator token

	r := p.parseCompareBinOp()
	if r == nil {
		return nil
	}

	return p.node(&CompareOpNode{Kind: k, Left: l, Right: r}, start)
}

func (p *Parser) parseLogicalAnd() parsedExprNode {
	start := p.peek()
	l := p.parseCompareBinOp()
	if l == nil {
		return nil
	}
	if p.peek().Kind != TokenKindAnd {
		return l
	}
	p.next() // eat &&
	r := p.parseLogicalAnd()
	if r == nil {
		return nil
	}
	return p.node(&LogicalOpNode{Kind: LogicalOpNodeKindAnd, Left: l, Right: r}, start)
}

func (p *Parser) parseLogicalOr() parsedExprNode {
	start := p.peek()
	l := p.parseLogicalAnd()
	if l == nil {
		return nil
	}
	if p.peek().Kind != TokenKindOr {
		return l
	}
	p.next() // eat ||
	r := p.parseLogicalOr()
	if r == nil {
		return nil
	}
	return p.node(&LogicalOpNode{Kind: LogicalOpNodeKindOr, Left: l, Right: r}, start)
}

// Err returns an error which was caused while previous parsing.
func (p *Parser) Err() *Error {
	if err := p.lexer.Err(); err != nil {
		return err
	}
	return p.err
}

// Parse parses token sequence lexed by a given lexer into syntax tree.
func (p *Parser) Parse(l *Lexer) (Node, *Error) {
	// Init
	p.err = nil
	p.lexer = l
	p.cur = l.Next()

	root := p.parseLogicalOr()
	if err := p.Err(); err != nil {
		return nil, err
	}

	if t := p.peek(); t.Kind != TokenKindEnd {
		// It did not reach the end of sequence
		qb := stringutil.QuotesBuilder{}
		qb.Append(t.Kind.String())
		c := 1
		for {
			t := l.Next()
			if t.Kind == TokenKindEnd {
				break
			}
			qb.Append(t.Kind.String())
			c++
		}
		p.errorf("parser did not reach end of input after parsing the expression. %d remaining token(s) in the input: %s", c, qb.Build())
		return nil, p.err
	}
	if node := expressionExceedingDepth(root); node != nil {
		p.err = errorAtToken(syntax, node.Token(), "exceeded max expression depth 50")
		return nil, p.err
	}

	return root, nil
}

// The runner limits the evaluated expression tree to 50 levels. Its Parser
// flattens adjacent && and || nodes, so a long chain of one logical operator
// consumes only one level. Parentheses themselves do not add tree nodes.
func expressionExceedingDepth(root Node) Node {
	type entry struct {
		node  Node
		depth int
	}
	stack := []entry{{root, 1}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current.depth > 50 {
			return current.node
		}
		push := func(child Node) {
			depth := current.depth + 1
			if parent, ok := current.node.(*LogicalOpNode); ok {
				if nested, ok := child.(*LogicalOpNode); ok && nested.Kind == parent.Kind {
					depth = current.depth
				}
			}
			stack = append(stack, entry{child, depth})
		}
		switch node := current.node.(type) {
		case *ObjectDerefNode:
			push(node.Receiver)
		case *ArrayDerefNode:
			push(node.Receiver)
		case *IndexAccessNode:
			push(node.Operand)
			push(node.Index)
		case *NotOpNode:
			push(node.Operand)
		case *CompareOpNode:
			push(node.Left)
			push(node.Right)
		case *LogicalOpNode:
			push(node.Left)
			push(node.Right)
		case *FuncCallNode:
			for _, arg := range node.Args {
				push(arg)
			}
		}
	}
	return nil
}

// ParseAssignedExpression accepts one complete expression, excluding interpolation.
func ParseAssignedExpression(s string) Node {
	if !strings.HasPrefix(s, "${{") {
		return nil
	}
	lex := NewLexer(s[3:])
	expr, err := NewParser().Parse(lex)
	if err != nil || lex.Offset() != len(s)-3 {
		return nil
	}
	return expr
}

// IsMappingInsertion recognizes insert only as the complete expression in a mapping key.
//
// Mapping insertion syntax reference (Azure Pipelines):
// https://learn.microsoft.com/en-us/azure/devops/pipelines/process/template-expressions?view=azure-devops&source=recommendations#insertion
func IsMappingInsertion(s string) bool {
	node, ok := ParseAssignedExpression(strings.TrimSpace(s)).(*VariableNode)
	return ok && node.Token().Value == "insert"
}

// LiteralExpressionValue evaluates a single string literal and requires complete input consumption.
func LiteralExpressionValue(s string) *string {
	if !strings.HasPrefix(s, "${{") {
		return nil
	}
	lex := NewLexer(s[3:])
	expr, err := NewParser().Parse(lex)
	if err != nil || lex.Offset() != len(s)-3 {
		return nil
	}
	if literal, ok := expr.(*StringNode); ok {
		return &literal.Value
	}
	return nil
}
