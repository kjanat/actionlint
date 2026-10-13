package gitrepo

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	asFile = false
	asDir  = true
)

type patternCase struct {
	path  string
	isDir bool
	want  patternMatch
}

func (m patternMatch) String() string {
	return [...]string{"none", "positive", "negative"}[m]
}

func list(buffer []byte, ignoreCase bool) *patternList {
	patterns := newPatternList(ignoreCase)
	patterns.addBuffer(buffer)
	return patterns
}

func check(t *testing.T, buffer string, cases ...patternCase) {
	t.Helper()
	checkBytes(t, []byte(buffer), false, cases...)
}

func checkIgnoreCase(t *testing.T, buffer string, cases ...patternCase) {
	t.Helper()
	checkBytes(t, []byte(buffer), true, cases...)
}

func checkBytes(t *testing.T, buffer []byte, ignoreCase bool, cases ...patternCase) {
	t.Helper()
	patterns := list(buffer, ignoreCase)
	for _, tc := range cases {
		if got := patterns.matched([]byte(tc.path), tc.isDir); got != tc.want {
			t.Errorf("%q (ignore case: %v) against %q (dir: %v) = %v, want %v", buffer, ignoreCase, tc.path, tc.isDir, got, tc.want)
		}
	}
}

func TestPatternListCounts(t *testing.T) {
	patterns := list([]byte("\n# comment\n   \n\\#a\n!b\n!\nc/\n\r\n"), false)
	if len(patterns.patterns) != 4 {
		t.Fatalf("%d patterns, want 4", len(patterns.patterns))
	}
	if patterns := list([]byte("# only\n\n"), false); len(patterns.patterns) != 0 {
		t.Fatalf("%d patterns, want 0", len(patterns.patterns))
	}
	if got := newPatternList(true).matched([]byte("a"), asFile); got != patternNone {
		t.Fatalf("empty list matched %v", got)
	}
}

func TestPatternNamesAtAnyDepth(t *testing.T) {
	check(t, "foo",
		patternCase{"foo", asFile, patternPositive},
		patternCase{"foo", asDir, patternPositive},
		patternCase{"a/foo", asFile, patternPositive},
		patternCase{"a/b/foo", asDir, patternPositive},
		patternCase{"foobar", asFile, patternNone},
		patternCase{"foo/bar", asFile, patternNone},
		patternCase{"", asFile, patternNone},
	)
}

func TestPatternAnchoredWithSlash(t *testing.T) {
	check(t, "/foo", patternCase{"foo", asFile, patternPositive}, patternCase{"a/foo", asFile, patternNone})
	check(t, "a/b", patternCase{"a/b", asFile, patternPositive}, patternCase{"a/b", asDir, patternPositive}, patternCase{"x/a/b", asFile, patternNone}, patternCase{"a/b/c", asFile, patternNone})
	check(t, "/a/b", patternCase{"a/b", asFile, patternPositive}, patternCase{"x/a/b", asFile, patternNone})
	check(t, "a[/x]b", patternCase{"axb", asFile, patternPositive}, patternCase{"q/axb", asFile, patternNone})
	check(t, "a\\/b", patternCase{"a/b", asFile, patternPositive}, patternCase{"x/a/b", asFile, patternNone})
	check(t, "//a", patternCase{"a", asFile, patternNone})
	check(t, "a//b", patternCase{"a/b", asFile, patternNone})
	check(t, "/", patternCase{"a", asDir, patternNone})
}

func TestPatternDirectoriesOnly(t *testing.T) {
	check(t, "foo/", patternCase{"foo", asDir, patternPositive}, patternCase{"foo", asFile, patternNone}, patternCase{"a/foo", asDir, patternPositive}, patternCase{"foo/a", asFile, patternNone})
	check(t, "doc/frotz/", patternCase{"doc/frotz", asDir, patternPositive}, patternCase{"doc/frotz", asFile, patternNone}, patternCase{"a/doc/frotz", asDir, patternNone})
	check(t, "a//", patternCase{"a", asDir, patternNone}, patternCase{"x/a", asDir, patternNone})
	check(t, "a/ ", patternCase{"a", asDir, patternPositive}, patternCase{"x/a", asDir, patternPositive})
	check(t, "a\\\\/", patternCase{"a\\", asDir, patternPositive}, patternCase{"a\\", asFile, patternNone})
}

func TestPatternSingleAsterisks(t *testing.T) {
	check(t, "*.ts",
		patternCase{"a.ts", asFile, patternPositive},
		patternCase{"x/y/a.ts", asFile, patternPositive},
		patternCase{".ts", asFile, patternPositive},
		patternCase{"a.tsx", asFile, patternNone},
		patternCase{"a.ts/b", asFile, patternNone},
	)
	check(t, "*", patternCase{"a", asFile, patternPositive}, patternCase{"a/b", asDir, patternPositive}, patternCase{".hidden", asFile, patternPositive})
	check(t, "foo/*", patternCase{"foo/a", asFile, patternPositive}, patternCase{"foo/a", asDir, patternPositive}, patternCase{"foo/a/b", asFile, patternNone}, patternCase{"foo", asDir, patternNone})
	check(t, "/*.c", patternCase{"a.c", asFile, patternPositive}, patternCase{"x/a.c", asFile, patternNone})
	check(t, "a*", patternCase{"a", asFile, patternPositive}, patternCase{"abc", asFile, patternPositive}, patternCase{"x/abc", asFile, patternPositive}, patternCase{"ba", asFile, patternNone})
	check(t, "a*b*c", patternCase{"abc", asFile, patternPositive}, patternCase{"axxbyyc", asFile, patternPositive}, patternCase{"acb", asFile, patternNone})
	check(t, "a/*b", patternCase{"a/b", asFile, patternPositive}, patternCase{"a/xb", asFile, patternPositive}, patternCase{"a/x/b", asFile, patternNone})
	check(t, "x/*", patternCase{"x/.a", asFile, patternPositive})
}

func TestPatternQuestionMarks(t *testing.T) {
	check(t, "?", patternCase{"a", asFile, patternPositive}, patternCase{"x/a", asFile, patternPositive}, patternCase{"ab", asFile, patternNone})
	check(t, "a?b", patternCase{"axb", asFile, patternPositive}, patternCase{"a/b", asFile, patternNone})
	check(t, "?a", patternCase{".a", asFile, patternPositive})
}

func TestPatternDoubleAsterisks(t *testing.T) {
	check(t, "**/foo", patternCase{"foo", asFile, patternPositive}, patternCase{"a/foo", asFile, patternPositive}, patternCase{"a/b/foo", asDir, patternPositive}, patternCase{"afoo", asFile, patternNone})
	check(t, "**/foo/bar", patternCase{"foo/bar", asFile, patternPositive}, patternCase{"a/foo/bar", asFile, patternPositive}, patternCase{"foo/x/bar", asFile, patternNone})
	check(t, "abc/**", patternCase{"abc", asDir, patternNone}, patternCase{"abc/x", asFile, patternPositive}, patternCase{"abc/x/y", asFile, patternPositive}, patternCase{"x/abc/y", asFile, patternNone})
	check(t, "a/**/b",
		patternCase{"a/b", asFile, patternPositive},
		patternCase{"a/x/b", asFile, patternPositive},
		patternCase{"a/x/y/b", asFile, patternPositive},
		patternCase{"a/xb", asFile, patternNone},
		patternCase{"ab", asFile, patternNone},
		patternCase{"a/x/b/c", asFile, patternNone},
	)
	check(t, "a/**/b/**/c", patternCase{"a/b/c", asFile, patternPositive}, patternCase{"a/x/b/y/z/c", asFile, patternPositive}, patternCase{"a/b/b/c", asFile, patternPositive}, patternCase{"a/c", asFile, patternNone})
	check(t, "a/**/", patternCase{"a/b", asDir, patternPositive}, patternCase{"a/b", asFile, patternNone}, patternCase{"a", asDir, patternNone})
	check(t, "**", patternCase{"a", asFile, patternPositive}, patternCase{"a/b", asFile, patternPositive})
	check(t, "/**", patternCase{"a", asFile, patternPositive}, patternCase{"a/b", asDir, patternPositive})
	check(t, "/**/", patternCase{"a", asDir, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "**/", patternCase{"a", asDir, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "**/**", patternCase{"a", asFile, patternPositive}, patternCase{"a/b", asFile, patternPositive})
	check(t, "**/*", patternCase{"a", asFile, patternPositive}, patternCase{"x/y", asFile, patternPositive})
	check(t, "**/a/**", patternCase{"a/b", asFile, patternPositive}, patternCase{"x/y/a/b/c", asFile, patternPositive}, patternCase{"a", asDir, patternNone})
	check(t, "x/**/*.ts", patternCase{"x/a.ts", asFile, patternPositive}, patternCase{"x/y/a.ts", asFile, patternPositive}, patternCase{"y/x/a.ts", asFile, patternNone})
}

func TestPatternOtherDoubleAsterisks(t *testing.T) {
	check(t, "a/**b", patternCase{"a/b", asFile, patternPositive}, patternCase{"a/xb", asFile, patternPositive}, patternCase{"a/x/b", asFile, patternNone})
	check(t, "a**/b", patternCase{"a/b", asFile, patternPositive}, patternCase{"ax/b", asFile, patternPositive}, patternCase{"ax/y/b", asFile, patternNone})
	check(t, "**a/b", patternCase{"a/b", asFile, patternPositive}, patternCase{"xa/b", asFile, patternPositive}, patternCase{"x/a/b", asFile, patternNone})
	check(t, "a/b**/c", patternCase{"a/bx/c", asFile, patternPositive}, patternCase{"a/bx/y/c", asFile, patternNone})
	check(t, "**x", patternCase{"y/zx", asFile, patternPositive}, patternCase{"x", asFile, patternPositive})
	check(t, "a***b", patternCase{"ab", asFile, patternPositive}, patternCase{"axyb", asFile, patternPositive})
	check(t, "\\**/a", patternCase{"*x/a", asFile, patternPositive}, patternCase{"x/y/a", asFile, patternNone}, patternCase{"a", asFile, patternNone})
	check(t, "a/\\**/b", patternCase{"a/*x/b", asFile, patternPositive}, patternCase{"a/x/b", asFile, patternNone})
}

func TestPatternLongerAsteriskRuns(t *testing.T) {
	check(t, "***/a", patternCase{"a", asFile, patternPositive}, patternCase{"x/y/a", asFile, patternPositive})
	check(t, "a/***", patternCase{"a/b/c", asFile, patternPositive}, patternCase{"a", asDir, patternNone})
	check(t, "a/****/b", patternCase{"a/b", asFile, patternPositive}, patternCase{"a/x/y/b", asFile, patternPositive})
}

func TestPatternDoubleAsterisksBeforeEscapedSlash(t *testing.T) {
	check(t, "a/**\\/b", patternCase{"a/b", asFile, patternNone}, patternCase{"a/x/b", asFile, patternPositive}, patternCase{"a/x/y/b", asFile, patternPositive}, patternCase{"a/xb", asFile, patternNone})
	check(t, "**\\/b", patternCase{"b", asFile, patternNone}, patternCase{"x/b", asFile, patternPositive}, patternCase{"x/y/b", asDir, patternPositive})
	check(t, "a\\/***\\/b", patternCase{"a/b", asFile, patternNone}, patternCase{"a/x/y/b", asFile, patternPositive})
	check(t, "**\\/**", patternCase{"a", asFile, patternNone}, patternCase{"a/b", asFile, patternPositive})
	check(t, "x**\\/b", patternCase{"x/b", asFile, patternPositive}, patternCase{"xy/b", asFile, patternPositive}, patternCase{"xy/z/b", asFile, patternNone})
	check(t, "a/**\\\\/b", patternCase{"a/x\\/b", asFile, patternPositive}, patternCase{"a/x/b", asFile, patternNone})
	check(t, "a\\/**", patternCase{"a/b/c", asFile, patternPositive}, patternCase{"a", asDir, patternNone})
	check(t, "\\/**", patternCase{"a", asFile, patternNone}, patternCase{"a/b", asFile, patternNone})
	check(t, "//**", patternCase{"a", asFile, patternNone}, patternCase{"a/b", asFile, patternNone})
	check(t, "a/**\\/", patternCase{"a/b/c", asDir, patternNone})
}

func TestPatternBracketExpressions(t *testing.T) {
	check(t, "[ab]", patternCase{"a", asFile, patternPositive}, patternCase{"x/b", asFile, patternPositive}, patternCase{"c", asFile, patternNone})
	check(t, "[!ab]", patternCase{"a", asFile, patternNone}, patternCase{"c", asFile, patternPositive})
	check(t, "[^ab]", patternCase{"a", asFile, patternNone}, patternCase{"c", asFile, patternPositive})
	check(t, "[]a]", patternCase{"]", asFile, patternPositive}, patternCase{"a", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	check(t, "[!]a]", patternCase{"]", asFile, patternNone}, patternCase{"a", asFile, patternNone}, patternCase{"b", asFile, patternPositive})
	check(t, "[a-c]", patternCase{"b", asFile, patternPositive}, patternCase{"d", asFile, patternNone}, patternCase{"-", asFile, patternNone})
	check(t, "[a-]", patternCase{"a", asFile, patternPositive}, patternCase{"-", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	check(t, "[-a]", patternCase{"a", asFile, patternPositive}, patternCase{"-", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	check(t, "[--0]", patternCase{"-", asFile, patternPositive}, patternCase{"0", asFile, patternPositive}, patternCase{"1", asFile, patternNone})
	check(t, "[]-a]", patternCase{"]", asFile, patternPositive}, patternCase{"^", asFile, patternPositive}, patternCase{"a", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	check(t, "[a-c-e]", patternCase{"b", asFile, patternPositive}, patternCase{"-", asFile, patternPositive}, patternCase{"e", asFile, patternPositive}, patternCase{"d", asFile, patternNone})
	check(t, "[!a-c]", patternCase{"a", asFile, patternNone}, patternCase{"d", asFile, patternPositive})
	check(t, "[a!]", patternCase{"!", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	check(t, "[!!]", patternCase{"!", asFile, patternNone}, patternCase{"a", asFile, patternPositive})
	check(t, "a[!x]b", patternCase{"ayb", asFile, patternPositive}, patternCase{"axb", asFile, patternNone}, patternCase{"a/b", asFile, patternNone})
	check(t, "a[%-0]b", patternCase{"a.b", asFile, patternPositive}, patternCase{"a/b", asFile, patternNone})
}

func TestPatternReversedRanges(t *testing.T) {
	check(t, "[c-a]", patternCase{"c", asFile, patternPositive}, patternCase{"a", asFile, patternNone}, patternCase{"b", asFile, patternNone})
	check(t, "[d-ax]", patternCase{"d", asFile, patternPositive}, patternCase{"x", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
}

func TestPatternEscapesInBrackets(t *testing.T) {
	check(t, "[\\]]", patternCase{"]", asFile, patternPositive}, patternCase{"\\", asFile, patternNone})
	check(t, "[\\a]", patternCase{"a", asFile, patternPositive}, patternCase{"\\", asFile, patternNone})
	check(t, "[a\\-c]", patternCase{"a", asFile, patternPositive}, patternCase{"-", asFile, patternPositive}, patternCase{"c", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	check(t, "[\\a-\\c]", patternCase{"b", asFile, patternPositive}, patternCase{"\\", asFile, patternNone})
	check(t, "[\\]-a]", patternCase{"^", asFile, patternPositive}, patternCase{"\\", asFile, patternNone})
	check(t, "[\\!]", patternCase{"!", asFile, patternPositive}, patternCase{"\\", asFile, patternNone})
}

func TestPatternCharacterClasses(t *testing.T) {
	check(t, "[[:digit:]]", patternCase{"1", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "[[:alpha:]x]", patternCase{"a", asFile, patternPositive}, patternCase{"x", asFile, patternPositive}, patternCase{"1", asFile, patternNone})
	check(t, "[[:digit:][:alpha:]]", patternCase{"1", asFile, patternPositive}, patternCase{"a", asFile, patternPositive}, patternCase{"-", asFile, patternNone})
	check(t, "[[:digit:]-z]", patternCase{"1", asFile, patternPositive}, patternCase{"-", asFile, patternPositive}, patternCase{"z", asFile, patternPositive}, patternCase{"m", asFile, patternNone})
	check(t, "[[:alnum:]]", patternCase{"Z", asFile, patternPositive}, patternCase{"5", asFile, patternPositive}, patternCase{"_", asFile, patternNone})
	check(t, "[[:blank:]]", patternCase{" ", asFile, patternPositive}, patternCase{"\t", asFile, patternPositive}, patternCase{"\n", asFile, patternNone})
	check(t, "[[:cntrl:]]", patternCase{"\x01", asFile, patternPositive}, patternCase{"\x7f", asFile, patternPositive}, patternCase{" ", asFile, patternNone})
	check(t, "[[:graph:]]", patternCase{"!", asFile, patternPositive}, patternCase{"~", asFile, patternPositive}, patternCase{" ", asFile, patternNone})
	check(t, "[[:lower:]]", patternCase{"a", asFile, patternPositive}, patternCase{"A", asFile, patternNone})
	check(t, "[[:upper:]]", patternCase{"A", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "[[:print:]]", patternCase{" ", asFile, patternPositive}, patternCase{"a", asFile, patternPositive}, patternCase{"\x01", asFile, patternNone})
	check(t, "a[[:punct:]]", patternCase{"a.", asFile, patternPositive}, patternCase{"a_", asFile, patternPositive}, patternCase{"aa", asFile, patternNone})
	check(t, "[[:space:]]",
		patternCase{" ", asFile, patternPositive},
		patternCase{"\t", asFile, patternPositive},
		patternCase{"\n", asFile, patternPositive},
		patternCase{"\r", asFile, patternPositive},
		patternCase{"\x0b", asFile, patternNone},
		patternCase{"\x0c", asFile, patternNone},
	)
	check(t, "[[:xdigit:]]", patternCase{"f", asFile, patternPositive}, patternCase{"F", asFile, patternPositive}, patternCase{"g", asFile, patternNone})
	check(t, "[[:", patternCase{"[", asFile, patternNone})
	check(t, "[[:]", patternCase{":", asFile, patternPositive}, patternCase{"[", asFile, patternPositive})
	check(t, "[[:a]", patternCase{":", asFile, patternPositive}, patternCase{"a", asFile, patternPositive}, patternCase{"[", asFile, patternPositive})
	check(t, "[[.a.]]", patternCase{"a]", asFile, patternPositive}, patternCase{".]", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "[[=a=]]", patternCase{"=]", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
}

func TestPatternInvalidNeverMatches(t *testing.T) {
	for _, pattern := range []string{"[a", "x[", "[]", "[!]", "x*[a", "[a]*[b", "[a\\]", "[[:foo:]]", "[[:foo:]a]", "[[:ALPHA:]]", "[[:alpha:]", "a\\", "\\"} {
		patterns := list([]byte(pattern), false)
		for _, path := range []string{"a", "[a", "x", "x[", "xa", "[]", "f", "]", "a\\", "\\", "ab", "a[b"} {
			if got := patterns.matched([]byte(path), asFile); got != patternNone {
				t.Errorf("%q against %q = %v, want none", pattern, path, got)
			}
		}
	}
}

func TestPatternEscapes(t *testing.T) {
	check(t, "\\*", patternCase{"*", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "\\?", patternCase{"?", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "\\[a]", patternCase{"[a]", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "\\a", patternCase{"a", asFile, patternPositive}, patternCase{"\\a", asFile, patternNone})
	check(t, "a\\\\", patternCase{"a\\", asFile, patternPositive})
	check(t, "\\#a", patternCase{"#a", asFile, patternPositive})
	check(t, "\\!a", patternCase{"!a", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "a/\\*", patternCase{"a/*", asFile, patternPositive}, patternCase{"a/b", asFile, patternNone})
}

func TestPatternBracesAreLiteral(t *testing.T) {
	check(t, "*.{ts,js}", patternCase{"a.{ts,js}", asFile, patternPositive}, patternCase{"a.ts", asFile, patternNone})
}

func TestPatternCommentsAndBlanks(t *testing.T) {
	check(t, "#a\n \n", patternCase{"#a", asFile, patternNone}, patternCase{" ", asFile, patternNone})
	check(t, " #a", patternCase{" #a", asFile, patternPositive}, patternCase{"#a", asFile, patternNone})
	check(t, "a#b", patternCase{"a#b", asFile, patternPositive})
}

func TestPatternTrailingSpaces(t *testing.T) {
	check(t, "a  ", patternCase{"a", asFile, patternPositive}, patternCase{"a ", asFile, patternNone})
	check(t, "a\\  ", patternCase{"a ", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "a \\ ", patternCase{"a  ", asFile, patternPositive})
	check(t, "a\\ \\ ", patternCase{"a  ", asFile, patternPositive})
	check(t, "a\\\\ ", patternCase{"a\\", asFile, patternPositive})
	check(t, "a\\\\\\ ", patternCase{"a\\ ", asFile, patternPositive})
	check(t, "\\ ", patternCase{" ", asFile, patternPositive})
	check(t, " a", patternCase{" a", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "a\t", patternCase{"a\t", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "a b", patternCase{"a b", asFile, patternPositive})
}

func TestPatternCarriageReturns(t *testing.T) {
	check(t, "a\r\nb\r", patternCase{"a", asFile, patternPositive}, patternCase{"b", asFile, patternPositive})
	check(t, "a\r\r\n", patternCase{"a\r", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "a \r", patternCase{"a", asFile, patternPositive})
	check(t, "a\r \n", patternCase{"a\r", asFile, patternPositive})
	check(t, "a\rb", patternCase{"a\rb", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "a\\\r", patternCase{"a", asFile, patternNone}, patternCase{"a\\", asFile, patternNone}, patternCase{"a\r", asFile, patternNone})
	check(t, "\r\n", patternCase{"\r", asFile, patternNone})
}

func TestPatternByteOrderMark(t *testing.T) {
	check(t, "\uFEFFa", patternCase{"a", asFile, patternPositive})
	check(t, "\uFEFF#a", patternCase{"#a", asFile, patternNone})
	check(t, "x\n\uFEFFa", patternCase{"\uFEFFa", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "\uFEFF\uFEFFa", patternCase{"\uFEFFa", asFile, patternPositive})
	patterns := newPatternList(false)
	patterns.addLine([]byte("\uFEFFa"))
	if got := patterns.matched([]byte("\uFEFFa"), asFile); got != patternPositive {
		t.Fatalf("addLine keeps the byte order mark: %v", got)
	}
}

func TestPatternNegation(t *testing.T) {
	check(t, "!a", patternCase{"a", asFile, patternNegative}, patternCase{"b", asFile, patternNone})
	check(t, "!!a", patternCase{"!a", asFile, patternNegative}, patternCase{"a", asFile, patternNone})
	check(t, "!\\ ", patternCase{" ", asFile, patternNegative})
	check(t, "!", patternCase{"a", asFile, patternNone})
	check(t, "!/", patternCase{"a", asDir, patternNone})
	check(t, "*\n!a/", patternCase{"a", asDir, patternNegative}, patternCase{"a", asFile, patternPositive}, patternCase{"b", asDir, patternPositive})
}

func TestPatternLastMatchWins(t *testing.T) {
	check(t, "a\nb\n!a\n!*.c\na.c",
		patternCase{"a", asFile, patternNegative},
		patternCase{"b", asFile, patternPositive},
		patternCase{"a.c", asFile, patternPositive},
		patternCase{"b.c", asFile, patternNegative},
		patternCase{"c", asFile, patternNone},
	)
	check(t, "/*\n!/foo\n/foo/*\n!/foo/bar",
		patternCase{"x", asFile, patternPositive},
		patternCase{"foo", asDir, patternNegative},
		patternCase{"foo/x", asFile, patternPositive},
		patternCase{"foo/bar", asDir, patternNegative},
	)
}

func TestPatternFoldsCase(t *testing.T) {
	checkIgnoreCase(t, "ABC\nX/abc\n*.TS\nFoo/\nÉ",
		patternCase{"abc", asFile, patternPositive},
		patternCase{"aBc", asFile, patternPositive},
		patternCase{"x/ABC", asFile, patternPositive},
		patternCase{"a.ts", asFile, patternPositive},
		patternCase{"b/a.Ts", asFile, patternPositive},
		patternCase{"foo", asDir, patternPositive},
		patternCase{"é", asFile, patternNone},
		patternCase{"É", asFile, patternPositive},
	)
	checkIgnoreCase(t, "[[:upper:]]", patternCase{"a", asFile, patternPositive}, patternCase{"A", asFile, patternPositive})
	checkIgnoreCase(t, "[A-C]", patternCase{"b", asFile, patternPositive}, patternCase{"d", asFile, patternNone})
	checkIgnoreCase(t, "[Z-a]", patternCase{"_", asFile, patternPositive}, patternCase{"z", asFile, patternPositive}, patternCase{"A", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	checkIgnoreCase(t, "[!a]", patternCase{"A", asFile, patternNone}, patternCase{"b", asFile, patternPositive})
	checkIgnoreCase(t, "[!A-Z]", patternCase{"a", asFile, patternNone}, patternCase{"1", asFile, patternPositive})
	checkIgnoreCase(t, "/Src/**", patternCase{"src/a", asFile, patternPositive}, patternCase{"SRC/A/B", asFile, patternPositive})
	checkIgnoreCase(t, "a/B/c", patternCase{"A/b/C", asFile, patternPositive})
	checkIgnoreCase(t, "a*B", patternCase{"AxB", asFile, patternPositive}, patternCase{"axb", asFile, patternPositive})
	checkIgnoreCase(t, "AB*", patternCase{"abc", asFile, patternPositive}, patternCase{"x/aBc", asFile, patternPositive})
	check(t, "ABC", patternCase{"abc", asFile, patternNone})
}

func TestPatternFoldsOnlyPathBytesForEscapes(t *testing.T) {
	checkIgnoreCase(t, "\\a", patternCase{"a", asFile, patternPositive}, patternCase{"A", asFile, patternPositive})
	checkIgnoreCase(t, "\\A", patternCase{"a", asFile, patternNone}, patternCase{"A", asFile, patternNone})
	checkIgnoreCase(t, "a\\A", patternCase{"aA", asFile, patternNone}, patternCase{"aa", asFile, patternNone})
	checkIgnoreCase(t, "\\x\\y", patternCase{"XY", asFile, patternPositive})
	checkIgnoreCase(t, "/\\A/b", patternCase{"A/b", asFile, patternNone}, patternCase{"a/b", asFile, patternNone})
	checkIgnoreCase(t, "*\\Ab", patternCase{"xAb", asFile, patternNone})
	checkIgnoreCase(t, "[\\a]", patternCase{"a", asFile, patternPositive}, patternCase{"A", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	checkIgnoreCase(t, "[A]", patternCase{"a", asFile, patternNone}, patternCase{"A", asFile, patternNone})
	checkIgnoreCase(t, "[\\A]", patternCase{"a", asFile, patternNone}, patternCase{"A", asFile, patternNone})
	checkIgnoreCase(t, "[!A]", patternCase{"a", asFile, patternPositive}, patternCase{"A", asFile, patternPositive})
	checkIgnoreCase(t, "[\\A-\\C]", patternCase{"a", asFile, patternPositive}, patternCase{"B", asFile, patternPositive}, patternCase{"x", asFile, patternNone})
	checkIgnoreCase(t, "[a-\\C]", patternCase{"a", asFile, patternPositive}, patternCase{"A", asFile, patternPositive}, patternCase{"b", asFile, patternNone})
	check(t, "\\A", patternCase{"A", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
	check(t, "[A]", patternCase{"A", asFile, patternPositive}, patternCase{"a", asFile, patternNone})
}

func TestPatternMatchesBytes(t *testing.T) {
	check(t, "?", patternCase{"é", asFile, patternNone})
	check(t, "??", patternCase{"é", asFile, patternPositive})
	check(t, "[!a][!a]", patternCase{"é", asFile, patternPositive})
	check(t, "[[:alpha:]]?", patternCase{"é", asFile, patternNone})
	checkBytes(t, []byte("[\xc3-\xc4]?"), false, patternCase{"é", asFile, patternPositive})
	patterns := newPatternList(false)
	patterns.addLine([]byte("a\xff*"))
	if got := patterns.matched([]byte("x/a\xffb"), asFile); got != patternPositive {
		t.Fatalf("high byte prefix = %v", got)
	}
	if got := patterns.matched([]byte("a\xfe"), asFile); got != patternNone {
		t.Fatalf("other high byte = %v", got)
	}
}

func TestPatternPolynomialTime(t *testing.T) {
	text := strings.Repeat("a", 2000)
	check(t, strings.Repeat("*a", 40)+"b", patternCase{text, asFile, patternNone})
	check(t, strings.Repeat("*a", 40)+"*", patternCase{text, asFile, patternPositive})
	deep := strings.TrimSuffix(strings.Repeat("a/", 400), "/")
	check(t, "/"+strings.Repeat("**/a/", 30)+"b", patternCase{deep, asFile, patternNone})
	check(t, "/"+strings.Repeat("**/a/", 30)+"a", patternCase{deep, asFile, patternPositive})
}

type rng uint64

func (r *rng) next() uint64 {
	x := uint64(*r)
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*r = rng(x)
	return x
}

func (r *rng) below(bound int) int { return int(r.next() % uint64(bound)) }

func (r *rng) pick(items []string) string { return items[r.below(len(items))] }

type atom struct {
	text    string
	samples []string
}

var atoms = []atom{
	{"a", []string{"a", "A"}}, {"b", []string{"b"}}, {"A", []string{"A", "a"}}, {"x", []string{"x"}}, {".", []string{"."}}, {"-", []string{"-"}},
	{"*", []string{"", "a", "ab", "b.a"}}, {"**", []string{"", "a", "a/b"}}, {"***", []string{"", "a", "a/b"}}, {"?", []string{"a", "."}}, {"/", []string{"/"}},
	{"[ab]", []string{"a", "b"}}, {"[!a]", []string{"b", "A"}}, {"[^b]", []string{"a", "b"}}, {"[a-b]", []string{"b", "B"}}, {"[A-Z]", []string{"Q", "q"}},
	{"[]a]", []string{"]", "a"}}, {"[a-]", []string{"-"}}, {"[b-a]", []string{"b", "a"}}, {"[[:upper:]]", []string{"A", "a"}}, {"[[:alpha:]]", []string{"x"}},
	{"[[:punct:]]", []string{".", "#"}}, {"[\\A]", []string{"A", "a"}}, {"[A]", []string{"A", "a"}}, {"\\a", []string{"a", "A"}}, {"\\A", []string{"A", "a"}},
	{"\\[", []string{"["}}, {"\\/", []string{"/"}}, {"\\*", []string{"x"}}, {"[", []string{"["}}, {"]", []string{"]"}}, {"\\", []string{"x"}}, {" ", []string{" "}},
	{"\\ ", []string{" "}}, {"#", []string{"#"}}, {"!", []string{"!"}}, {"{a,b}", []string{"{a,b}", "a"}},
}

var rareAtoms = []atom{
	{"[[:", []string{"["}}, {"[!]a]", []string{"b", "]"}}, {"[--0]", []string{"-", "0"}}, {"[a-c-e]", []string{"e", "-", "d"}}, {"[\\]]", []string{"]"}},
	{"[[:digit:]-z]", []string{"-", "z"}}, {"[[.a.]]", []string{"a]"}}, {"[[:foo:]]", []string{"f"}}, {"**/", []string{"", "a/"}}, {"/**/", []string{"/", "/a/"}},
	{"\\#", []string{"#"}}, {"\\!", []string{"!"}}, {"?*", []string{"a", "ab"}}, {"\\\\", []string{"x"}},
}

var names = []string{"a", "b", "A", "x", "ab", "ba", "aa", "aB", "Ab", "a.b", ".a", "b.a", "a-b", "[a]", "]", "{a,b}", "a b", "#", "!a"}

func randomPattern(r *rng) (string, []string) {
	rare := r.below(4) == 0
	var prefix, suffix string
	if rare {
		prefix = r.pick([]string{"\\/", "//"})
		suffix = r.pick([]string{"\\ ", " \\ ", "\\", "//"})
	} else {
		prefix = r.pick([]string{"/", "**/", "", "", ""})
		suffix = r.pick([]string{"/", "/**", "  ", "", "", ""})
	}
	pool := atoms
	if rare {
		pool = rareAtoms
	}
	count := 1 + r.below(4)
	picked := make([]atom, 0, count)
	for range count {
		picked = append(picked, pool[r.below(len(pool))])
	}
	var pattern strings.Builder
	pattern.WriteString(prefix)
	for _, a := range picked {
		pattern.WriteString(a.text)
	}
	pattern.WriteString(suffix)
	var samples []string
	for range 3 {
		var sample strings.Builder
		if prefix == "**/" {
			sample.WriteString(r.pick([]string{"", "x/", "a/b/"}))
		}
		for _, a := range picked {
			sample.WriteString(r.pick(a.samples))
		}
		if suffix == "/**" {
			sample.WriteString(r.pick([]string{"/a", "/b/A"}))
		}
		var parts []string
		for name := range strings.SplitSeq(sample.String(), "/") {
			if name != "" && name != "." && !strings.HasSuffix(name, " ") && !strings.HasSuffix(name, ".") {
				parts = append(parts, name)
			}
		}
		if joined := strings.Join(parts, "/"); joined != "" {
			samples = append(samples, joined)
		}
	}
	return pattern.String(), samples
}

func randomPath(r *rng, maxDepth int) string {
	depth := 1 + r.below(maxDepth)
	parts := make([]string, 0, depth)
	for range depth {
		parts = append(parts, r.pick(names))
	}
	return strings.Join(parts, "/")
}

func lineEnding(r *rng) string {
	if r.below(4) == 0 {
		return "\r\n"
	}
	return "\n"
}

type gitCase struct {
	dir            string
	buffer         string
	patterns       *patternList
	folded         *patternList
	paths          []patternCase
	checkAncestors bool
}

func (c *gitCase) dirName(isDir bool) string {
	if isDir {
		return c.dir + "-dir"
	}
	return c.dir + "-file"
}

func (c *gitCase) expected(path string, isDir, ignoreCase bool) patternMatch {
	patterns := c.patterns
	if ignoreCase {
		patterns = c.folded
	}
	if c.checkAncestors {
		for index, b := range []byte(path) {
			if b == '/' && patterns.matched([]byte(path[:index]), asDir) == patternPositive {
				return patternPositive
			}
		}
	}
	return patterns.matched([]byte(path), isDir)
}

func TestPatternAgreesWithGitCheckIgnore(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("LC_ALL", "C")
	repo := filepath.Join(root, "repo")
	git := func(args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = repo
		out, err := command.Output()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 1 {
				return out
			}
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}
	if err := os.MkdirAll(repo, 0750); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")

	r := rng(0x9E3779B97F4A7C15)
	var cases []*gitCase
	for index := range 1500 {
		var buffer strings.Builder
		if r.below(10) == 0 {
			buffer.WriteString("\uFEFF")
		}
		pattern, samples := randomPattern(&r)
		if strings.HasPrefix(pattern, "!") {
			buffer.WriteString("\\")
		}
		buffer.WriteString(pattern)
		buffer.WriteString(lineEnding(&r))
		var paths []patternCase
		for range 6 {
			paths = append(paths, patternCase{path: randomPath(&r, 4), isDir: r.below(2) == 0})
		}
		for _, sample := range samples {
			paths = append(paths, patternCase{path: sample, isDir: asFile}, patternCase{path: sample, isDir: asDir})
		}
		text := buffer.String()
		cases = append(cases, &gitCase{dir: fmt.Sprintf("single%d", index), buffer: text, patterns: list([]byte(text), false), folded: list([]byte(text), true), paths: paths, checkAncestors: true})
	}
	for index := range 400 {
		var buffer strings.Builder
		var paths []patternCase
		for range 6 {
			paths = append(paths, patternCase{path: randomPath(&r, 1), isDir: r.below(2) == 0})
		}
		for range 2 + r.below(4) {
			if r.below(3) == 0 {
				buffer.WriteString("!")
			}
			pattern, samples := randomPattern(&r)
			buffer.WriteString(pattern)
			buffer.WriteString(lineEnding(&r))
			for _, sample := range samples {
				if !strings.Contains(sample, "/") {
					paths = append(paths, patternCase{path: sample, isDir: r.below(2) == 0})
				}
			}
		}
		text := buffer.String()
		cases = append(cases, &gitCase{dir: fmt.Sprintf("list%d", index), buffer: text, patterns: list([]byte(text), false), folded: list([]byte(text), true), paths: paths})
	}

	var queries bytes.Buffer
	for _, c := range cases {
		for _, isDir := range []bool{false, true} {
			dir := filepath.Join(repo, c.dirName(isDir))
			if err := os.MkdirAll(dir, 0750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(c.buffer), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, p := range c.paths {
			if p.isDir {
				if err := os.MkdirAll(filepath.Join(repo, c.dirName(true), filepath.FromSlash(p.path)), 0750); err != nil {
					t.Fatal(err)
				}
			}
			queries.WriteString(c.dirName(p.isDir))
			queries.WriteString("/")
			queries.WriteString(p.path)
			queries.WriteByte(0)
		}
	}
	queriesPath := filepath.Join(root, "queries")
	if err := os.WriteFile(queriesPath, queries.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	var mismatches []string
	matches, total := 0, 0
	for _, ignoreCase := range []bool{false, true} {
		command := exec.CommandContext(t.Context(), "git", "-c", fmt.Sprintf("core.ignoreCase=%v", ignoreCase), "check-ignore", "--no-index", "--stdin", "-z", "--verbose", "--non-matching")
		command.Dir = repo
		input, err := os.Open(queriesPath)
		if err != nil {
			t.Fatal(err)
		}
		command.Stdin = input
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("git check-ignore: %v: %s", err, stderr.String())
			}
		}
		fields := bytes.Split(output, []byte{0})
		field := 0
		for _, c := range cases {
			for _, p := range c.paths {
				if field+4 > len(fields) {
					t.Fatal("git printed fewer records than queries")
				}
				source, pattern := fields[field], fields[field+2]
				field += 4
				actual := patternNone
				switch {
				case len(source) == 0:
				case len(pattern) > 0 && pattern[0] == '!':
					actual = patternNegative
				default:
					actual = patternPositive
				}
				expected := c.expected(p.path, p.isDir, ignoreCase)
				total++
				if actual != patternNone {
					matches++
				}
				if actual != expected {
					mismatches = append(mismatches, fmt.Sprintf("%q (ignore case: %v) against %q (dir: %v): git %v, ours %v", c.buffer, ignoreCase, p.path, p.isDir, actual, expected))
				}
			}
		}
	}
	if len(mismatches) > 0 {
		shown := mismatches
		if len(shown) > 40 {
			shown = shown[:40]
		}
		t.Fatalf("%d mismatches:\n%s", len(mismatches), strings.Join(shown, "\n"))
	}
	if matches*6 <= total {
		t.Fatalf("only %d of %d queries matched", matches, total)
	}
}
