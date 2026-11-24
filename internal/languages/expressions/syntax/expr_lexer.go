package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
)

type TokenKind int

const (
	TokenKindUnknown TokenKind = iota
	TokenKindEnd
	TokenKindIdent
	TokenKindString
	// TokenKindInt includes hexadecimal integers.
	TokenKindInt
	TokenKindFloat
	TokenKindLeftParen
	TokenKindRightParen
	TokenKindLeftBracket
	TokenKindRightBracket
	TokenKindDot
	TokenKindNot
	TokenKindLess
	TokenKindLessEq
	TokenKindGreater
	TokenKindGreaterEq
	TokenKindEq
	TokenKindNotEq
	TokenKindAnd
	TokenKindOr
	TokenKindStar
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

type Token struct {
	Kind     TokenKind
	Value    string
	Offset   int
	Position sourcetext.Position
}

func (t *Token) String() string {
	return fmt.Sprintf("%s:%d:%d:%d", t.Kind.String(), t.Position.Line, t.Position.Column, t.Offset)
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

type Lexer struct {
	src    string
	scan   scanner.Scanner
	lexErr *SyntaxError
	start  scanner.Position
}

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
		lex.lexErr = &SyntaxError{
			Message:   msg,
			Offset:    p.Offset,
			EndOffset: end,
			Position:  sourcetext.Position{Line: p.Line, Column: p.Column},
		}
	}
}

func (lex *Lexer) token(kind TokenKind) *Token {
	p := lex.scan.Pos()
	s := lex.start
	t := &Token{
		Kind:     kind,
		Value:    lex.src[s.Offset:p.Offset],
		Offset:   s.Offset,
		Position: sourcetext.Position{Line: s.Line, Column: s.Column},
	}
	lex.start = p
	return t
}

func (lex *Lexer) eof() *Token {
	return &Token{
		Kind:     TokenKindEnd,
		Value:    "",
		Offset:   lex.start.Offset,
		Position: sourcetext.Position{Line: lex.start.Line, Column: lex.start.Column},
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

func (lex *Lexer) unexpected(r rune, where, expected string) *Token {
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
		if r := lex.eat(); !isAlnum(r) && r != '_' && r != '-' {
			return lex.token(TokenKindIdent)
		}
	}
}

func (lex *Lexer) lexNum() *Token {
	// GitHub Actions accepts 0x hex literals although its documentation allows JSON number formats only.

	r := lex.scan.Peek() // r is a digit or '-'

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

	// GitHub Actions rejects hex exponents such as 0x1f2p-a8.

	if isAlnum(r) {
		s := lex.src[lex.start.Offset:lex.scan.Pos().Offset]
		return lex.unexpected(r, "character following hex integer "+s, expectedPunctChars)
	}

	return lex.token(TokenKindInt)
}

func (lex *Lexer) lexString() *Token {
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

func (lex *Lexer) Next() *Token {
	lex.skipWhite()

	r := lex.scan.Peek()
	if r == scanner.EOF {
		return lex.unexpectedEOF()
	}

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

func (lex *Lexer) Offset() int {
	return lex.scan.Pos().Offset
}

// Err returns the first lexing error.
func (lex *Lexer) Err() *SyntaxError {
	return lex.lexErr.detached()
}

// LexExpression reports an unexpected EOF error when src lacks the closing '}}'.
func LexExpression(src string) ([]*Token, int, *SyntaxError) {
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
