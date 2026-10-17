package engine

import (
	"strings"
	"testing"
	"unicode/utf8"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	exprlang "actionlint.kjanat.dev/internal/expression"
)

func TestParserOwnedExpressionSpans(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{`fromJSON('{"win":1}')['mac']`, `fromJSON('{"win":1}')['mac']`},
		{`steps.foo.outputs['bar']`, `steps.foo.outputs['bar']`},
		{`matrix.x.y`, `matrix.x.y`},
		{`env[inputs.name] && true`, `env[inputs.name] && true`},
		{`fromJSON(format('{0}', '[]'))[0].name`, `fromJSON(format('{0}', '[]'))[0].name`},
		{`env[format('a]b', inputs.name)]`, `env[format('a]b', inputs.name)]`},
		{`startWith('hello')`, `startWith('hello')`},
		{`format('x', 1) && false`, `format('x', 1) && false`},
		{`(fromJSON('{}'))['mac']`, `(fromJSON('{}'))['mac']`},
		{`env . NAME [ 'x' ]`, `env . NAME [ 'x' ]`},
	} {
		t.Run(tc.text, func(t *testing.T) {
			node, err := exprlang.NewParser().Parse(exprlang.NewLexer(tc.text + "}}"))
			if err != nil {
				t.Fatal(err)
			}
			span := exprlang.ExpressionSpan(node)
			if got := tc.text[span.Start:span.End]; got != tc.want {
				t.Fatalf("range %q, want %q", got, tc.want)
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
			if strings.HasPrefix(expression, "steps.foo") {
				end = start + len("steps.foo")
			}
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

func TestFunctionDiagnosticRangeIgnoresResultAccess(t *testing.T) {
	for _, call := range []string{`format('{0}{1}', 1, 2, 3)`, `startWith('a', 'b')`, `startsWith('a')`} {
		for _, suffix := range []string{"", "[0]", ".x", "[0].x"} {
			src := "on: push\nenv:\n  VALUE: ${{ " + call + suffix + " }}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
			if err != nil {
				t.Fatal(err)
			}
			start := strings.Index(src, call)
			found := false
			for _, d := range result.Diagnostics {
				if strings.Contains(d.Message, "format string") || strings.Contains(d.Message, "undefined function") || strings.Contains(d.Message, "number of arguments") {
					found = true
					assertDiagnosticOffsetRange(t, src, d, start, start+strings.IndexByte(call, '('))
				}
			}
			if !found {
				t.Fatalf("missing function finding for %s%s: %+v", call, suffix, result.Diagnostics)
			}
		}
	}
}

func TestCompositeDeprecatedCommands(t *testing.T) {
	for _, mode := range []string{"enabled", "inline", "disabled", "nested", "override-off", "override-on"} {
		t.Run(mode, func(t *testing.T) {
			root, _ := executableFixture(t)
			config := "tools: {shellcheck: false, ruff: false}\n"
			if mode == "disabled" || mode == "override-on" {
				config += "lint: {rules: {correctness: {deprecated-commands: off}}}\n"
			}
			if mode == "override-off" {
				config += "overrides:\n  - includes: [inner/action.yml]\n    lint: {rules: {correctness: {deprecated-commands: off}}}\n"
			}
			if mode == "override-on" {
				config += "overrides:\n  - includes: [inner/action.yml]\n    lint: {rules: {correctness: {deprecated-commands: error}}}\n"
			}
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", config)
			body := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n"
			body += "    - shell: bash\n"
			if mode == "inline" {
				body += "      # actionlint:ignore-next-line deprecated-commands -- legacy fixture\n"
			}
			body += "      run: |\n        echo ::set-output name=x::1\n        echo ::set-output name=y::2\n"
			writeShellcheckFixture(t, root, "inner/action.yml", body)
			call := "- uses: ./inner"
			if mode == "nested" {
				writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
				call = "- uses: ./outer"
			}
			result := compositeAnalysis(t, root, call, AnalysisOptions{})
			count := 0
			for _, d := range result.Diagnostics {
				if d.Rule != "deprecated-commands" {
					continue
				}
				start := strings.Index(body, "set-output")
				if count == 1 {
					start += len("set-output") + strings.Index(body[start+len("set-output"):], "set-output")
				}
				assertDiagnosticOffsetRange(t, body, d, start, start+len("set-output"))
				count++
			}
			want := 2
			if mode == "inline" || mode == "disabled" || mode == "override-off" {
				want = 0
			}
			if count != want {
				t.Fatalf("got %d deprecated commands, want %d: %+v", count, want, result.Diagnostics)
			}
		})
	}
}

func assertDiagnosticOffsetRange(t *testing.T, src string, d diagmodel.Diagnostic, start, end int) {
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
		var findings []diagmodel.Diagnostic
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
