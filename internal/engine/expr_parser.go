package engine

import (
	"fmt"
	"strconv"
	"strings"
)

func errorAtToken(t *Token, msg string) *ExprError {
	return &ExprError{
		Message:   msg,
		Offset:    t.Offset,
		EndOffset: t.Offset + len(t.Value),
		Line:      t.Line,
		Column:    t.Column,
	}
}

// ExprParser is a parser for expression syntax. To know the details, see
// https://docs.github.com/en/actions/learn-github-actions/expressions
type ExprParser struct {
	cur   *Token
	lexer *ExprLexer
	err   *ExprError
	last  *Token
}

// NewExprParser creates new ExprParser instance.
func NewExprParser() *ExprParser {
	return &ExprParser{}
}

func (p *ExprParser) error(msg string) {
	if p.err == nil {
		p.err = errorAtToken(p.cur, msg)
	}
}

func (p *ExprParser) errorf(format string, args ...any) {
	p.error(fmt.Sprintf(format, args...))
}

func (p *ExprParser) unexpected(where string, expected []TokenKind) {
	if p.err != nil {
		return
	}
	qb := quotesBuilder{}
	for _, e := range expected {
		qb.append(e.String())
	}
	var what string
	if p.cur.Kind == TokenKindEnd {
		what = "end of input"
	} else {
		what = fmt.Sprintf("token %q", p.cur.Kind.String())
	}
	msg := fmt.Sprintf("unexpected %s while parsing %s. expecting %s", what, where, qb.build())
	p.error(msg)
}

func (p *ExprParser) next() *Token {
	ret := p.cur
	p.last = ret
	p.cur = p.lexer.Next()
	return ret
}

type parsedExprNode interface {
	ExprNode
	setSpan(ExprSpan)
}

func (p *ExprParser) node(n parsedExprNode, start *Token) parsedExprNode {
	n.setSpan(ExprSpan{start.Offset, p.last.Offset + len(p.last.Value), start.Line, start.Column})
	return n
}

func (p *ExprParser) peek() *Token {
	return p.cur
}

func (p *ExprParser) parseIdent() parsedExprNode {
	ident := p.next() // eat ident
	switch p.peek().Kind {
	case TokenKindLeftParen:
		// Parse function call as primary expression though generally function call is parsed as
		// postfix expression. The reason is that only built-in function call is allowed in workflow
		// expression syntax, meant that callee is always built-in function name, not a general
		// expression.
		p.next() // eat '('
		args := []ExprNode{}
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

				switch p.peek().Kind {
				case TokenKindComma:
					p.next() // eat ','
					// continue to next argument
				case TokenKindRightParen:
					p.next() // eat ')'
					break LoopArgs
				default:
					p.unexpected("arguments of function call", []TokenKind{TokenKindComma, TokenKindRightParen})
					return nil
				}
			}
		}
		return p.node(&FuncCallNode{Callee: ident.Value, Args: args, tok: ident}, ident)
	default:
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
}

func (p *ExprParser) parseNestedExpr() parsedExprNode {
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

func (p *ExprParser) parseInt() parsedExprNode {
	t := p.peek()
	i, err := strconv.ParseInt(t.Value, 0, 32)
	if err != nil {
		p.errorf("parsing invalid integer literal %q: %s", t.Value, err)
		return nil
	}

	p.next() // eat int

	return p.node(&IntNode{Value: int(i), tok: t}, t)
}

func (p *ExprParser) parseFloat() parsedExprNode {
	t := p.peek()
	f, err := strconv.ParseFloat(t.Value, 64)
	if err != nil {
		p.errorf("parsing invalid float literal %q: %s", t.Value, err)
		return nil
	}

	p.next() // eat float

	return p.node(&FloatNode{Value: f, tok: t}, t)
}

func (p *ExprParser) parseString() parsedExprNode {
	t := p.next() // eat string
	s := t.Value
	s = s[1 : len(s)-1]                  // strip first and last single quotes
	s = strings.ReplaceAll(s, "''", "'") // unescape ''
	return p.node(&StringNode{Value: s, tok: t}, t)
}

func (p *ExprParser) parsePrimaryExpr() parsedExprNode {
	switch p.peek().Kind {
	case TokenKindIdent:
		return p.parseIdent()
	case TokenKindLeftParen:
		return p.parseNestedExpr()
	case TokenKindInt:
		return p.parseInt()
	case TokenKindFloat:
		return p.parseFloat()
	case TokenKindString:
		return p.parseString()
	default:
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
}

func (p *ExprParser) parsePostfixOp() parsedExprNode {
	start := p.peek()
	ret := p.parsePrimaryExpr()
	if ret == nil {
		return nil
	}

	for {
		switch p.peek().Kind {
		case TokenKindDot:
			p.next() // eat '.'
			switch p.peek().Kind {
			case TokenKindStar:
				p.next() // eat '*'
				ret = p.node(&ArrayDerefNode{Receiver: ret}, start)
			case TokenKindIdent:
				t := p.next() // eat 'b' of 'a.b'
				// Property name is case insensitive. github.event and github.EVENT are the same
				ret = p.node(&ObjectDerefNode{Receiver: ret, Property: strings.ToLower(t.Value)}, start)
			default:
				p.unexpected(
					"object property dereference like 'a.b' or array element dereference like 'a.*'",
					[]TokenKind{TokenKindIdent, TokenKindStar},
				)
				return nil
			}
		case TokenKindLeftBracket:
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
		default:
			return ret
		}
	}
}

func (p *ExprParser) parsePrefixOp() parsedExprNode {
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

func (p *ExprParser) parseCompareBinOp() parsedExprNode {
	start := p.peek()
	l := p.parsePrefixOp()
	if l == nil {
		return nil
	}

	var k CompareOpNodeKind
	switch p.peek().Kind {
	case TokenKindLess:
		k = CompareOpNodeKindLess
	case TokenKindLessEq:
		k = CompareOpNodeKindLessEq
	case TokenKindGreater:
		k = CompareOpNodeKindGreater
	case TokenKindGreaterEq:
		k = CompareOpNodeKindGreaterEq
	case TokenKindEq:
		k = CompareOpNodeKindEq
	case TokenKindNotEq:
		k = CompareOpNodeKindNotEq
	default:
		return l
	}
	p.next() // eat the operator token

	r := p.parseCompareBinOp()
	if r == nil {
		return nil
	}

	return p.node(&CompareOpNode{Kind: k, Left: l, Right: r}, start)
}

func (p *ExprParser) parseLogicalAnd() parsedExprNode {
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

func (p *ExprParser) parseLogicalOr() parsedExprNode {
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
func (p *ExprParser) Err() *ExprError {
	if err := p.lexer.Err(); err != nil {
		return err
	}
	return p.err
}

// Parse parses token sequence lexed by a given lexer into syntax tree.
func (p *ExprParser) Parse(l *ExprLexer) (ExprNode, *ExprError) {
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
		qb := quotesBuilder{}
		qb.append(t.Kind.String())
		c := 1
		for {
			t := l.Next()
			if t.Kind == TokenKindEnd {
				break
			}
			qb.append(t.Kind.String())
			c++
		}
		p.errorf("parser did not reach end of input after parsing the expression. %d remaining token(s) in the input: %s", c, qb.build())
		return nil, p.err
	}
	if node := expressionExceedingDepth(root); node != nil {
		p.err = errorAtToken(node.Token(), "exceeded max expression depth 50")
		return nil, p.err
	}

	return root, nil
}

// The runner limits the evaluated expression tree to 50 levels. Its parser
// flattens adjacent && and || nodes, so a long chain of one logical operator
// consumes only one level. Parentheses themselves do not add tree nodes.
func expressionExceedingDepth(root ExprNode) ExprNode {
	type entry struct {
		node  ExprNode
		depth int
	}
	stack := []entry{{root, 1}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current.depth > 50 {
			return current.node
		}
		push := func(child ExprNode) {
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

// parseAssignedExpression accepts one complete expression, excluding interpolation.
func parseAssignedExpression(s string) ExprNode {
	if !strings.HasPrefix(s, "${{") {
		return nil
	}
	lex := NewExprLexer(s[3:])
	expr, err := NewExprParser().Parse(lex)
	if err != nil || lex.Offset() != len(s)-3 {
		return nil
	}
	return expr
}

// insert is a directive only when it is the complete expression in a mapping key.
// Elsewhere it remains an ordinary variable subject to expression validation.
//
// Mapping insertion syntax reference (Azure Pipelines):
// https://learn.microsoft.com/en-us/azure/devops/pipelines/process/template-expressions?view=azure-devops&source=recommendations#insertion
func isMappingInsertion(s string) bool {
	node, ok := parseAssignedExpression(strings.TrimSpace(s)).(*VariableNode)
	return ok && node.Token().Value == "insert"
}

// literalExpressionValue evaluates a single string literal and requires complete input consumption.
func literalExpressionValue(s string) *string {
	if !strings.HasPrefix(s, "${{") {
		return nil
	}
	lex := NewExprLexer(s[3:])
	expr, err := NewExprParser().Parse(lex)
	if err != nil || lex.Offset() != len(s)-3 {
		return nil
	}
	if literal, ok := expr.(*StringNode); ok {
		return &literal.Value
	}
	return nil
}
