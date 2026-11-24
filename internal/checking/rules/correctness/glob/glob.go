// Package glob validates GitHub workflow glob syntax.
package glob

import (
	"fmt"
	"strings"
	"text/scanner"
	"unicode"
)

type InvalidGlobPatternError struct {
	Message string
	// Column is 1-based, and zero means the error occurred before the first character or after a newline.
	Column int
}

func (err *InvalidGlobPatternError) Error() string {
	return fmt.Sprintf("%d: %s", err.Column, err.Message)
}

func (err *InvalidGlobPatternError) String() string {
	return err.Error()
}

type globValidator struct {
	isRef bool
	prec  bool
	errs  []InvalidGlobPatternError
	scan  scanner.Scanner
}

func (v *globValidator) error(msg string) {
	p := v.scan.Pos()
	// The scanner has already consumed the character at the error position.
	c := p.Column - 1
	if p.Line > 1 {
		c = 0
	}
	v.errs = append(v.errs, InvalidGlobPatternError{msg, c})
}

func (v *globValidator) unexpected(char rune, what, why string) {
	unexpected := "unexpected EOF"
	if char != scanner.EOF {
		unexpected = fmt.Sprintf("unexpected character %q", char)
	}

	while := ""
	if what != "" {
		while = " while checking " + what
	}

	v.error(fmt.Sprintf("invalid glob pattern. %s%s. %s", unexpected, while, why))
}

func (v *globValidator) invalidRefChar(c rune, why string) {
	cfmt := "%q"
	if unicode.IsPrint(c) {
		cfmt = "'%c'" // avoid '\\'
	}
	format := "character " + cfmt + " is invalid for branch and tag names. %s. see `man git-check-ref-format` for more details. note that regular expression is unavailable"
	msg := fmt.Sprintf(format, c, why)
	v.error(msg)
}

func (v *globValidator) init(pat string) {
	v.errs = []InvalidGlobPatternError{}
	v.prec = false
	v.scan.Init(strings.NewReader(pat))
	v.scan.Error = func(s *scanner.Scanner, m string) {
		v.error(fmt.Sprintf("error while scanning glob pattern %q: %s", pat, m))
	}
}

func (v *globValidator) validateNext() bool {
	c := v.scan.Next()
	prec := true

	switch c {
	case '\\':
		switch v.scan.Peek() {
		case '[', '?', '*':
			c = v.scan.Next()
			if v.isRef {
				v.invalidRefChar(v.scan.Peek(), "ref name cannot contain spaces, ~, ^, :, [, ?, *")
			}
		case '+', '\\', '!':
			c = v.scan.Next()
		default:
			// file path can contain '\' (`mkdir 'foo\bar'` works)
			if v.isRef {
				v.invalidRefChar('\\', "only special characters [, ?, +, *, \\, ! can be escaped with \\")
				c = v.scan.Next()
			}
		}
	case '?':
		if !v.prec {
			v.unexpected('?', "special character ? (zero or one)", "the preceding character must not be special character")
		}
		prec = false
	case '+':
		if !v.prec {
			v.unexpected('+', "special character + (one or more)", "the preceding character must not be special character")
		}
		prec = false
	case '*':
		prec = false
	case '[':
		if v.scan.Peek() == ']' {
			c = v.scan.Next()
			v.unexpected(']', "content of character match []", "character match must not be empty")
			break
		}

		chars := 0
	Loop:
		for {
			c = v.scan.Next()
			switch c {
			case ']':
				break Loop
			case scanner.EOF:
				v.unexpected(c, "end of character match []", "missing ]")
				return false
			default:
				if v.scan.Peek() != '-' {
					chars++
					continue Loop
				}

				chars += 2 // The range matches one or more characters. The later check only tests chars > 1.
				s := c
				v.scan.Next()
				switch v.scan.Peek() {
				case ']':
					c = v.scan.Next()
					v.unexpected(c, "character range in []", "end of range is missing")
					break Loop
				case scanner.EOF:
				default:
					c = v.scan.Next()
					if s > c {
						why := fmt.Sprintf("start of range %q (%d) is larger than end of range %q (%d)", s, s, c, c)
						v.unexpected(c, "character range in []", why)
					}
				}
			}
		}

		if chars == 1 {
			v.unexpected(c, "character match []", "character match with single character is useless. simply use x instead of [x]")
		}
	case '\r':
		if v.scan.Peek() == '\n' {
			c = v.scan.Next()
		}
		v.unexpected(c, "", "newline cannot be contained")
	case '\n':
		v.unexpected('\n', "", "newline cannot be contained")
	case ' ', '\t', '~', '^', ':':
		if v.isRef {
			v.invalidRefChar(c, "ref name cannot contain spaces, ~, ^, :, [, ?, *")
		}
	default:
	}
	v.prec = prec

	if v.scan.Peek() == scanner.EOF {
		if v.isRef && (c == '/' || c == '.') {
			v.invalidRefChar(c, "ref name must not end with / and .")
		}
		return false
	}

	return true
}

func (v *globValidator) validate(pat string) {
	v.init(pat)

	if pat == "" {
		v.error("glob pattern cannot be empty")
		return
	}

	switch v.scan.Peek() {
	case '/':
		if v.isRef {
			v.scan.Next()
			v.invalidRefChar('/', "ref name must not start with /")
			v.prec = true
		}
	case '!':
		v.scan.Next()
		if v.scan.Peek() == scanner.EOF {
			v.unexpected('!', "! at first character (negate pattern)", "at least one character must follow !")
			return
		}
		v.prec = false
	}

	for v.validateNext() {
	}
}

func Validate(pat string, isRef bool) []InvalidGlobPatternError {
	v := globValidator{}
	v.isRef = isRef
	v.validate(pat)
	return v.errs
}

func ValidateRefGlob(pat string) []InvalidGlobPatternError {
	return Validate(pat, true)
}

func ValidatePathGlob(pat string) []InvalidGlobPatternError {
	p := strings.TrimSpace(pat)

	var errs []InvalidGlobPatternError
	if pat != p {
		errs = append(errs, InvalidGlobPatternError{"leading and trailing spaces are not allowed in glob path", 0})
	}

	// '.' is not handled by path filter (#521)
	p = strings.TrimPrefix(p, "!")
	if p == "." || p == ".." || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
		errs = append(errs, InvalidGlobPatternError{"'.' and '..' are not allowed in glob path", 0})
	}

	if len(errs) > 0 {
		return errs
	}

	return Validate(pat, false)
}
