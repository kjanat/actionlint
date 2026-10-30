package parser

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

// GitHub normalises plain YAML booleans in if: before compiling the condition.
// Quoted spellings stay strings and are checked as expressions.
func TestParserConditionNormalisesYAMLBooleans(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"True", "true"},
		{"FALSE", "false"},
		{"true", "true"},
		{"null", "success()"},
		{"~", "success()"},
		{`"True"`, "True"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			source := "on: push\njobs:\n  check:\n    if: " + tc.value + "\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			root, err := codec.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			workflow, findings := parseDocument([]byte(source), root)
			if len(findings) != 0 {
				t.Fatal(findings)
			}
			if condition := workflow.Jobs["check"].If; condition == nil || condition.Value != tc.want {
				t.Fatalf("if: %s parsed as %+v, want %q", tc.value, condition, tc.want)
			}
		})
	}
}
