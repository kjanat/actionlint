package engine

import (
	"strings"
	"testing"
	"unicode/utf8"

	diagmodel "actionlint.kjanat.dev/diagnostic"
)

func TestMetadataFoldedConditionDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		condition string
		want      int
	}{
		{"${{ 'secrets.A || secrets.A' }}", 1},
		{"${{ 'secrets.A || vars.A' }}", 2},
		{"${{ secrets.A || secrets.A }}", 2},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n      if: "+tc.condition+"\n")
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("want %d diagnostics, got %+v", tc.want, result.Diagnostics)
			}
		})
	}
}

func TestMetadataExpressionDiagnosticPositions(t *testing.T) {
	const composite = "runs:\n  using: composite\n  steps:\n    - shell: bash\n      "
	for _, tc := range []struct{ name, body, token, message string }{
		{"context", composite + "run: echo ${{ forbidden_context.value }}\n", "forbidden_context", "not available"},
		{"unknown-function", composite + "run: echo ${{ nonexistentFunction() }}\n", "nonexistentFunction", "unknown function"},
		{"arity", composite + "run: echo ${{ contains('a') }}\n", "contains", "invalid number"},
		{"special-function", composite + "run: echo ${{ always() }}\n", "always", "not available"},
		{"json-collision", composite + "run: |\n        echo ${{ fromJSON('{\"a\":1,\"A\":2}') }}\n", "fromJSON", "collide"},
		{"syntax", composite + "run: echo ${{ github.ref + 'main' }}\n", "+", "invalid expression"},
		{"syntax-delimiter", composite + "run: echo ${{ github.ref. }}\n", "}}", "invalid expression"},
		{"later-syntax", composite + "run: echo ${{ github.ref }} ${{ github.ref + 'main' }}\n", "+", "invalid expression"},
		{"syntax-eof", composite + "run: echo ${{ github.ref", "github.ref", "invalid expression"},
		{"syntax-eof-unclosed-string", composite + "run: echo ${{ 'abc }}", "'abc", "invalid expression"},
		{"syntax-eof-double-quoted", composite + "run: \"echo ${{ github.ref\n        \"\n", "github.ref", "invalid expression"},
		{"syntax-eof-single-quoted", composite + "run: 'echo ${{ github.ref\n        '\n", "github.ref", "invalid expression"},
		{"syntax-eof-literal", composite + "run: |\n        echo ${{ github.ref\n", "github.ref", "invalid expression"},
		{"syntax-eof-folded", composite + "run: >\n        echo ${{ github.ref\n", "github.ref", "invalid expression"},
		{"syntax-eof-literal-no-newline", composite + "run: |\n        echo ${{ github.ref", "github.ref", "invalid expression"},
		{"syntax-eof-folded-no-newline", composite + "run: >\n        echo ${{ github.ref", "github.ref", "invalid expression"},
		{"bare-syntax", composite + "run: echo ok\n      if: github.ref + 'main'\n", "+", "invalid expression"},
		{"later-context", composite + "run: echo ${{ github.ref }} ${{ forbidden_context.value }}\n", "forbidden_context", "not available"},
		{"input-default", "inputs:\n  test:\n    description: test\n    default: prefix ${{ forbidden_context.value }}\n" + composite + "run: echo ok\n", "forbidden_context", "not available"},
		{"schema-value", "outputs:\n  test:\n    description: test\n    value: prefix ${{ forbidden_context.value }}\n" + composite + "run: echo ok\n", "forbidden_context", "not available"},
		{"schema-key", "runs:\n  using: docker\n  image: docker://alpine\n  env:\n    prefix_${{ forbidden_context.value }}: value\n", "forbidden_context", "not available in keys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\n" + tc.body
			writeShellcheckFixture(t, root, "inner/action.yml", metadata)
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			var matched []diagmodel.Diagnostic
			for _, d := range result.Diagnostics {
				if strings.Contains(d.Message, tc.message) {
					matched = append(matched, d)
				}
			}
			if len(matched) != 1 {
				t.Fatalf("want one %q diagnostic: %+v", tc.message, result.Diagnostics)
			}
			if tc.message == "invalid expression" {
				_, detail, ok := strings.Cut(matched[0].Message, "has an invalid expression: ")
				if !ok || detail == "" || detail[0] >= '0' && detail[0] <= '9' {
					t.Fatalf("syntax message includes raw coordinates or lacks detail: %s", matched[0].Message)
				}
			}
			var want diagmodel.Position
			for i, line := range strings.Split(metadata, "\n") {
				if col := strings.Index(line, tc.token); col >= 0 {
					want = diagmodel.Position{Line: i + 1, Column: utf8.RuneCountInString(line[:col]) + 1}
					if strings.HasPrefix(tc.name, "syntax-eof") {
						want.Column = utf8.RuneCountInString(line) + 1
					}
					break
				}
			}
			if matched[0].Start != want {
				t.Fatalf("want %+v got %+v: %s", want, matched[0].Start, matched[0].Message)
			}
			if tc.name == "syntax-delimiter" && matched[0].End != (diagmodel.Position{Line: want.Line, Column: want.Column + 2}) {
				t.Fatalf("closing delimiter must retain its two-character range: %+v", matched[0])
			}
			if strings.HasPrefix(tc.name, "syntax-eof") && matched[0].End != want {
				t.Fatalf("EOF must have a zero-width range at %+v, got %+v", want, matched[0])
			}
		})
	}
}
