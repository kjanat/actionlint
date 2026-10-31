package expression

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"
	"unicode/utf8"
)

// TokenKind is kind of Token.
type TokenKind int

const (
	// TokenKindUnknown is a default value of Token as unknown Token value.
	TokenKindUnknown TokenKind = iota
	// TokenKindEnd is a Token for end of Token sequence. Sequence without this
	// Token means invalid.
	TokenKindEnd
	// TokenKindIdent is a Token for identifier.
	TokenKindIdent
	// TokenKindString is a Token for String literals.
	TokenKindString
	// TokenKindInt is a Token for integers including hex integers.
	TokenKindInt
	// TokenKindFloat is a Token for float numbers.
	TokenKindFloat
	// TokenKindLeftParen is a Token for '('.
	TokenKindLeftParen
	// TokenKindRightParen is a Token for ')'.
	TokenKindRightParen
	// TokenKindLeftBracket is a Token for '['.
	TokenKindLeftBracket
	// TokenKindRightBracket is a Token for ']'.
	TokenKindRightBracket
	// TokenKindDot is a Token for '.'.
	TokenKindDot
	// TokenKindNot is a Token for '!'.
	TokenKindNot
	// TokenKindLess is a Token for '<'.
	TokenKindLess
	// TokenKindLessEq is a Token for '<='.
	TokenKindLessEq
	// TokenKindGreater is a Token for '>'.
	TokenKindGreater
	// TokenKindGreaterEq is a Token for '>='.
	TokenKindGreaterEq
	// TokenKindEq is a Token for '=='.
	TokenKindEq
	// TokenKindNotEq is a Token for '!='.
	TokenKindNotEq
	// TokenKindAnd is a Token for '&&'.
	TokenKindAnd
	// TokenKindOr is a Token for '||'.
	TokenKindOr
	// TokenKindStar is a Token for '*'.
	TokenKindStar
	// TokenKindComma is a Token for ','.
	TokenKindComma
)

func (t TokenKind) String() string {
	switch t {
	case TokenKindUnknown:
		return "UNKNOWN"
	case TokenKindEnd:
		return "END"
	case TokenKindIdent:
		return "IDENT"
	case TokenKindString:
		return "STRING"
	case TokenKindInt:
		return "INTEGER"
	case TokenKindFloat:
		return "FLOAT"
	case TokenKindLeftParen:
		return "("
	case TokenKindRightParen:
		return ")"
	case TokenKindLeftBracket:
		return "["
	case TokenKindRightBracket:
		return "]"
	case TokenKindDot:
		return "."
	case TokenKindNot:
		return "!"
	case TokenKindLess:
		return "<"
	case TokenKindLessEq:
		return "<="
	case TokenKindGreater:
		return ">"
	case TokenKindGreaterEq:
		return ">="
	case TokenKindEq:
		return "=="
	case TokenKindNotEq:
		return "!="
	case TokenKindAnd:
		return "&&"
	case TokenKindOr:
		return "||"
	case TokenKindStar:
		return "*"
	case TokenKindComma:
		return ","
	default:
		panic("unreachable")
	}
}

// Token is a Token lexed from expression syntax. For more details, see
// https://docs.github.com/en/actions/learn-github-actions/expressions
type Token struct {
	// Kind is kind of the Token.
	Kind TokenKind
	// Value is String representation of the Token.
	Value string
	// Offset is byte Offset of Token String starting.
	Offset int
	// Line is line number of start position of the Token. Note that this value is 1-based.
	Line int
	// Column is column number of start position of the Token. Note that this value is 1-based.
	Column int
}

func (t *Token) String() string {
	return fmt.Sprintf("%s:%d:%d:%d", t.Kind.String(), t.Line, t.Column, t.Offset)
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\n' || r == '\r' || r == '\t'
}

func isAlpha(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z'
}

func isNum(r rune) bool {
	return '0' <= r && r <= '9'
}

func isHexNum(r rune) bool {
	return isNum(r) || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F'
}

func isAlnum(r rune) bool {
	return isAlpha(r) || isNum(r)
}

const (
	expectedPunctChars = "''', '}', '(', ')', '[', ']', '.', '!', '<', '>', '=', '&', '|', '*', ',', ' '"
	expectedDigitChars = "'0'..'9'"
	expectedAlphaChars = "'a'..'z', 'A'..'Z', '_'"
	expectedAllChars   = expectedAlphaChars + ", " + expectedDigitChars + ", " + expectedPunctChars
)

// Lexer is a struct to lex expression syntax. To know the syntax, see
// https://docs.github.com/en/actions/learn-github-actions/expressions
type Lexer struct {
	src    string
	scan   scanner.Scanner
	lexErr *Error
	start  scanner.Position
}

// NewLexer makes new Lexer instance.
func NewLexer(src string) *Lexer {
	l := &Lexer{
		src: src,
		start: scanner.Position{
			Offset: 0,
			Line:   1,
			Column: 1,
		},
	}
	l.scan.Init(strings.NewReader(src))
	l.scan.Error = func(_ *scanner.Scanner, m string) {
		l.error("scan error while lexing expression: " + m)
	}
	return l
}

func (lex *Lexer) error(msg string) {
	if lex.lexErr == nil {
		p := lex.scan.Pos()
		end := p.Offset
		if end < len(lex.src) {
			_, width := utf8.DecodeRuneInString(lex.src[end:])
			end += width
		}
		lex.lexErr = &Error{
			Message: msg,
			Rule:    syntax.rule, Code: syntax.code,
			Offset:    p.Offset,
			EndOffset: end,
			Line:      p.Line,
			Column:    p.Column,
		}
	}
}

func (lex *Lexer) token(kind TokenKind) *Token {
	p := lex.scan.Pos()
	s := lex.start
	t := &Token{
		Kind:   kind,
		Value:  lex.src[s.Offset:p.Offset],
		Offset: s.Offset,
		Line:   s.Line,
		Column: s.Column,
	}
	lex.start = p
	return t
}

func (lex *Lexer) eof() *Token {
	return &Token{
		Kind:   TokenKindEnd,
		Value:  "",
		Offset: lex.start.Offset,
		Line:   lex.start.Line,
		Column: lex.start.Column,
	}
}

func (lex *Lexer) eat() rune {
	lex.scan.Next()
	return lex.scan.Peek() // unlike lex.scan.Next(), return top char *after* eating
}

func (lex *Lexer) skipWhite() {
	for {
		if r := lex.scan.Peek(); !isWhitespace(r) {
			return
		}
		lex.scan.Next()
		lex.start = lex.scan.Pos()
	}
}

func (lex *Lexer) unexpected(r rune, where string, expected string) *Token {
	var what string
	if r == scanner.EOF {
		what = "EOF"
	} else {
		what = "character " + strconv.QuoteRune(r)
	}

	var note string
	if r == '"' {
		note = ". do you mean string literals? only single quotes are available for string delimiter"
	}

	msg := fmt.Sprintf(
		"got unexpected %s while lexing %s, expecting %s%s",
		what,
		where,
		expected,
		note,
	)

	lex.error(msg)
	return lex.eof()
}

func (lex *Lexer) unexpectedEOF() *Token {
	lex.error("unexpected EOF while lexing expression")
	return lex.eof()
}

func (lex *Lexer) lexIdent() *Token {
	for {
		// a-z, A-Z, 0-9, - or _
		// https://docs.github.com/en/actions/learn-github-actions/contexts
		if r := lex.eat(); !isAlnum(r) && r != '_' && r != '-' {
			return lex.token(TokenKindIdent)
		}
	}
}

func (lex *Lexer) lexNum() *Token {
	// The official document says number literals are 'Any number format supported by JSON' but actually
	// hex numbers starting with 0x are supported.

	r := lex.scan.Peek() // precond: r is digit or '-'

	if r == '-' {
		r = lex.eat()
	}

	if r == '0' {
		r = lex.eat()
		if r == 'x' {
			lex.scan.Next()
			return lex.lexHexInt()
		}
	} else {
		// r is 1..9
		if !isNum(r) {
			return lex.unexpected(r, "integer part of number", expectedDigitChars)
		}
		for {
			r = lex.eat()
			if !isNum(r) {
				break
			}
		}
	}

	k := TokenKindInt

	if r == '.' {
		r = lex.eat() // eat '.'
		if !isNum(r) {
			return lex.unexpected(r, "fraction part of float number", expectedDigitChars)
		}

		for {
			r = lex.eat()
			if !isNum(r) {
				break
			}
		}

		k = TokenKindFloat
	}

	if r == 'e' || r == 'E' {
		r = lex.eat() // eat 'e' or 'E'
		if r == '-' || r == '+' {
			r = lex.eat()
		}

		if !isNum(r) {
			return lex.unexpected(r, "exponent part of float number", expectedDigitChars)
		}
		for {
			r = lex.eat()
			if !isNum(r) {
				break
			}
		}

		k = TokenKindFloat
	}

	if isAlnum(r) {
		s := lex.src[lex.start.Offset:lex.scan.Pos().Offset]
		return lex.unexpected(r, "character following number "+s, expectedPunctChars)
	}

	return lex.token(k)
}

func (lex *Lexer) lexHexInt() *Token {
	r := lex.scan.Peek()

	if r == '0' {
		r = lex.eat()
	} else {
		if !isHexNum(r) {
			e := expectedDigitChars + ", 'a'..'f', 'A'..'F'"
			return lex.unexpected(r, "hex integer", e)
		}
		for {
			r = lex.eat()
			if !isHexNum(r) {
				break
			}
		}
	}

	// Note: GitHub Actions does not support exponent part like 0x1f2p-a8

	if isAlnum(r) {
		s := lex.src[lex.start.Offset:lex.scan.Pos().Offset]
		return lex.unexpected(r, "character following hex integer "+s, expectedPunctChars)
	}

	return lex.token(TokenKindInt)
}

func (lex *Lexer) lexString() *Token {
	// precond: current char is '
	for {
		switch lex.eat() {
		case '\'':
			if lex.eat() != '\'' { // when not escaped single quote ''
				return lex.token(TokenKindString)
			}
		case scanner.EOF:
			return lex.unexpected(scanner.EOF, "end of string literal", "'''")
		}
	}
}

func (lex *Lexer) lexEnd() *Token {
	r := lex.eat() // eat the first '}'
	if r != '}' {
		return lex.unexpected(r, "end marker }}", "'}'")
	}
	lex.scan.Next()
	// }} is an end marker of interpolation
	return lex.token(TokenKindEnd)
}

func (lex *Lexer) lexLess() *Token {
	k := TokenKindLess
	if lex.eat() == '=' { // eat '<'
		k = TokenKindLessEq
		lex.scan.Next()
	}
	return lex.token(k)
}

func (lex *Lexer) lexGreater() *Token {
	k := TokenKindGreater
	if lex.eat() == '=' { // eat '>'
		k = TokenKindGreaterEq
		lex.scan.Next()
	}
	return lex.token(k)
}

func (lex *Lexer) lexEq() *Token {
	if r := lex.eat(); r != '=' { // eat '='
		return lex.unexpected(r, "== operator", "'='")
	}
	lex.scan.Next()
	return lex.token(TokenKindEq)
}

func (lex *Lexer) lexBang() *Token {
	k := TokenKindNot
	if lex.eat() == '=' { // eat '!'
		lex.scan.Next() // eat '='
		k = TokenKindNotEq
	}
	return lex.token(k)
}

func (lex *Lexer) lexAnd() *Token {
	if r := lex.eat(); r != '&' { // eat the first '&'
		return lex.unexpected(r, "&& operator", "'&'")
	}
	lex.scan.Next() // eat the second '&'
	return lex.token(TokenKindAnd)
}

func (lex *Lexer) lexOr() *Token {
	if r := lex.eat(); r != '|' { // eat the first '|'
		return lex.unexpected(r, "|| operator", "'|'")
	}
	lex.scan.Next() // eat the second '|'
	return lex.token(TokenKindOr)
}

func (lex *Lexer) lexChar(k TokenKind) *Token {
	lex.scan.Next()
	return lex.token(k)
}

// Next lexes Next Token to lex input incrementally. Lexer must be initialized with Init() method
// before the first call of this method. This method is stateful. Lexer advances Offset by lexing
// Token. To get the Offset, use Offset() method.
func (lex *Lexer) Next() *Token {
	lex.skipWhite()

	r := lex.scan.Peek()
	if r == scanner.EOF {
		return lex.unexpectedEOF()
	}

	// Ident starts with a-z or A-Z or _
	// https://docs.github.com/en/actions/learn-github-actions/contexts
	if isAlpha(r) || r == '_' {
		return lex.lexIdent()
	}

	if isNum(r) || r == '-' {
		return lex.lexNum()
	}

	switch r {
	case '\'':
		return lex.lexString()
	case '}':
		return lex.lexEnd()
	case '!':
		return lex.lexBang()
	case '<':
		return lex.lexLess()
	case '>':
		return lex.lexGreater()
	case '=':
		return lex.lexEq()
	case '&':
		return lex.lexAnd()
	case '|':
		return lex.lexOr()
	case '(':
		return lex.lexChar(TokenKindLeftParen)
	case ')':
		return lex.lexChar(TokenKindRightParen)
	case '[':
		return lex.lexChar(TokenKindLeftBracket)
	case ']':
		return lex.lexChar(TokenKindRightBracket)
	case '.':
		return lex.lexChar(TokenKindDot)
	case '*':
		return lex.lexChar(TokenKindStar)
	case ',':
		return lex.lexChar(TokenKindComma)
	default:
		return lex.unexpected(r, "expression", expectedAllChars)
	}
}

// Offset returns the current Offset (scanning position).
func (lex *Lexer) Offset() int {
	return lex.scan.Pos().Offset
}

// Err returns an error while lexing. When multiple errors occur, the first one is returned.
func (lex *Lexer) Err() *Error {
	return lex.lexErr.detached()
}

// LexExpression lexes the given String as expression syntax. The parameter must contain '}}' which
// represents end of expression. Otherwise this function will report an error that it encountered
// unexpected EOF.
func LexExpression(src string) ([]*Token, int, *Error) {
	l := NewLexer(src)
	ts := []*Token{}
	for {
		t := l.Next()
		if l.lexErr != nil {
			return nil, l.scan.Pos().Offset, l.lexErr
		}
		ts = append(ts, t)
		if t.Kind == TokenKindEnd {
			return ts, l.scan.Pos().Offset, nil
		}
	}
}
