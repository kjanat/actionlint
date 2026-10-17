package actions_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/testutil"
)

func TestMetadataExpressionRangesAndOccurrences(t *testing.T) {
	const repeated = "${{ forbidden.value }} ${{ forbidden.value }}"
	const composite = "runs:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n"
	for _, tc := range []struct{ name, body, token string }{
		{"step", strings.Replace(composite, "echo ok", repeated, 1), "forbidden"},
		{"default", "inputs:\n  test:\n    default: " + repeated + "\n" + composite, "forbidden"},
		{"output", "outputs:\n  test:\n    value: " + repeated + "\n" + composite, "forbidden"},
		{"key", "runs:\n  using: docker\n  image: docker://alpine\n  env:\n    " + repeated + ": value\n", "forbidden"},
		{"functions", strings.Replace(composite, "echo ok", "${{ unknown() }} ${{ unknown() }}", 1), "unknown"},
		{"json-collisions", strings.Replace(composite, "echo ok", `${{ fromJSON('{"a":1,"A":2}') }} ${{ fromJSON('{"a":1,"A":2}') }}`, 1), "fromJSON"},
		{"escaped", strings.Replace(composite, "echo ok", `"${{ \u0066orbidden.value }} ${{ forbidden.value }}"`, 1), "forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\n" + tc.body
			testutil.WriteFixture(t, root, "inner/action.yml", metadata)
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if len(result.Diagnostics) != 2 {
				t.Fatalf("expected both occurrences: %+v", result.Diagnostics)
			}
			lines := strings.Split(metadata, "\n")
			for i, diagnostic := range result.Diagnostics {
				rawToken := tc.token
				if tc.name == "escaped" && i == 0 {
					rawToken = `\u0066orbidden`
				}
				line := []rune(lines[diagnostic.Start.Line-1])
				if diagnostic.End.Line != diagnostic.Start.Line || diagnostic.End.Column-diagnostic.Start.Column != utf8.RuneCountInString(rawToken) {
					t.Fatalf("incorrect token range: %+v", diagnostic)
				}
				if got := string(line[diagnostic.Start.Column-1 : diagnostic.End.Column-1]); got != rawToken {
					t.Fatalf("range selected %q, want %q", got, rawToken)
				}
			}
			if result.Diagnostics[0].Start == result.Diagnostics[1].Start {
				t.Fatal("distinct occurrences share a position")
			}
			encoded, err := json.Marshal(result.Diagnostics)
			if err != nil {
				t.Fatal(err)
			}
			var decoded []diagmodel.Diagnostic
			if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded) != 2 || decoded[1].End != result.Diagnostics[1].End {
				t.Fatalf("JSON lost ranges: %s, %v", encoded, err)
			}
		})
	}
}

func TestMetadataExpressionSyntaxRanges(t *testing.T) {
	for _, token := range []string{"+", "unexpected"} {
		t.Run(token, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			testutil.WriteFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ${{ github.ref "+token+" }}\n")
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want one syntax diagnostic: %+v", result.Diagnostics)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Start.Line != 7 || diagnostic.End.Line != 7 || diagnostic.Start.Column != 32 || diagnostic.End.Column != 32+len(token) {
				t.Fatalf("incorrect syntax token range: %+v", diagnostic)
			}
		})
	}
}
