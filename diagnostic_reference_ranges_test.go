package actionlint

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExpressionReferenceWidth(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{`fromJSON('{"win":1}')['mac']`, `fromJSON('{"win":1}')['mac']`},
		{`steps.foo.outputs['bar']`, `steps.foo.outputs['bar']`},
		{`matrix.x.y`, `matrix.x.y`},
		{`env[inputs.name] && true`, `env[inputs.name]`},
		{`fromJSON(format('{0}', '[]'))[0].name`, `fromJSON(format('{0}', '[]'))[0].name`},
		{`env[format('a]b', inputs.name)]`, `env[format('a]b', inputs.name)]`},
		{`startWith('hello')`, `startWith`},
		{`format('x', 1) && false`, `format`},
		{`env['unterminated`, `env`},
		{`env[inputs.name)`, `env`},
		{`env . NAME [ 'x' ]`, `env . NAME [ 'x' ]`},
	} {
		t.Run(tc.text, func(t *testing.T) {
			lexer := NewExprLexer(tc.text)
			first := lexer.Next()
			if got := expressionReferenceWidth(lexer, len(first.Value)); got != len(tc.want) {
				t.Fatalf("range %q, want %q", tc.text[:got], tc.want)
			}
		})
	}
}

func TestWorkflowIndexedReferenceDiagnosticRanges(t *testing.T) {
	for _, expression := range []string{
		`fromJSON('{"win":"windows-latest"}')['mac']`,
		`steps.foo.outputs['bar']`,
		`fromJSON('{"win":1}').mac`,
	} {
		for _, scalar := range []string{
			"${{ " + expression + " }}",
			">-\n      ${{ " + strings.ReplaceAll(expression, "[", "\n      [") + " }}",
		} {
			src := "on: push\nenv:\n  VALUE: " + scalar + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Index(src, "${{ ") + 4
			end := strings.Index(src, " }}")
			found := false
			for _, d := range result.Diagnostics {
				if strings.HasPrefix(d.Message, "property ") {
					found = true
					assertDiagnosticOffsetRange(t, src, d, start, end)
				}
			}
			if !found {
				t.Fatalf("missing property diagnostic: %+v", result.Diagnostics)
			}
		}
	}
}

func assertDiagnosticOffsetRange(t *testing.T, src string, d Diagnostic, start, end int) {
	t.Helper()
	position := func(offset int) (int, int) {
		return strings.Count(src[:offset], "\n") + 1, utf8.RuneCountInString(src[strings.LastIndexByte(src[:offset], '\n')+1:offset]) + 1
	}
	sl, sc := position(start)
	el, ec := position(end)
	if d.Start.Line != sl || d.Start.Column != sc || d.End.Line != el || d.End.Column != ec {
		t.Fatalf("want %d:%d-%d:%d, got %+v", sl, sc, el, ec, d)
	}
}

func TestDeprecatedCommandMappedOccurrences(t *testing.T) {
	for _, scalar := range []string{
		"|\n          echo ::set-output name=x::1\n          echo ::set-output name=y::2",
		">-\n          echo ::set-output name=x::1\n          echo ::set-output name=y::2",
		`"é\t::set-output name=x::1\n::set-output name=y::2"`,
		`'é ''quote'' ::set-output name=x::1 ::set-output name=y::2'`,
		"echo ::set-output name=x::1\n          ::set-output name=y::2",
	} {
		src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: " + scalar + "\n"
		result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
		if err != nil {
			t.Fatal(err)
		}
		var findings []Diagnostic
		for _, d := range result.Diagnostics {
			if d.Rule == "deprecated-commands" {
				findings = append(findings, d)
			}
		}
		if len(findings) != 2 {
			t.Fatalf("expected two commands, got %+v", findings)
		}
		first := strings.Index(src, "set-output")
		second := first + len("set-output") + strings.Index(src[first+len("set-output"):], "set-output")
		assertDiagnosticOffsetRange(t, src, findings[0], first, first+len("set-output"))
		assertDiagnosticOffsetRange(t, src, findings[1], second, second+len("set-output"))
	}
}
