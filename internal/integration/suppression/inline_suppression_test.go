package suppression_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/testutil"
)

func TestInlineSuppressionMultilinePlainComments(t *testing.T) {
	for _, value := range []string{
		"name: one\n  two",
		"name: !!str\n  example",
		"name: &title\n  example",
		"name: &title !!str\n  example",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			for _, tc := range []struct {
				name, rule, config, kind string
			}{
				{"unsupported", "unknown-rule", "", "inline-suppression"},
				{"prohibited", "cache-operation", "policy: {disallow-suppressions: true}", "disallow-suppressions"},
			} {
				t.Run(value+"/"+ending+"/"+tc.name, func(t *testing.T) {
					source := "on: push\n" + value + " # actionlint:ignore " + tc.rule + " -- reviewed\n"
					if strings.Contains(value, "&title") {
						source += "run-name: *title\n"
					}
					source += `jobs:
  test:
` + testutil.WorkflowSteps
					errs := testutil.WorkflowDiagnostics(t, strings.ReplaceAll(source, "\n", ending), tc.config)
					if len(errs) != 1 || errs[0].Rule != tc.kind || errs[0].Start.Line != 3 {
						t.Fatalf("want one %s on line 3; got %v", tc.kind, errs)
					}
				})
			}
		}
	}
}

func TestInlineSuppressionScalarPrefixes(t *testing.T) {
	const invalid = "# actionlint:ignore unknown-rule -- reviewed"
	const allowed = "# actionlint:ignore cache-operation -- reviewed"
	for _, tc := range []struct {
		name, value string
		line        int
	}{
		{"literal", "name: !!str\n  | " + invalid + "\n    title\n", 3},
		{"folded", "name: !!str\n  > " + invalid + "\n    title\n", 3},
		{"literal indicator", "name: !!str\n  |2- " + invalid + "\n    title\n", 3},
		{"property only", "name: !!str " + invalid + "\n  title\n", 2},
		{"property and value", "name: !!str " + invalid + "\n  title " + allowed + "\n", 2},
		{"property and block header", "name: !!str " + allowed + "\n  | " + invalid + "\n    title\n", 3},
		{"standalone before scalar", `name: !!str
  # actionlint:ignore-next-line unknown-rule -- reviewed
  title
`, 3},
		{"block body is text", "name: !!str\n  | " + invalid + "\n    title " + allowed + "\n", 3},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+"/"+ending, func(t *testing.T) {
				source := "on: push\n" + tc.value + `jobs:
  test:
` + testutil.WorkflowSteps
				errs := testutil.WorkflowDiagnostics(t, strings.ReplaceAll(source, "\n", ending), "")
				if len(errs) != 1 || errs[0].Rule != "inline-suppression" || errs[0].Start.Line != tc.line {
					t.Fatalf("want one inline-suppression at %d, got %v", tc.line, errs)
				}
			})
		}
	}
	// Identical comments on the property and value are separate directives.
	source := "on: push\nname: !!str " + allowed + "\n  title " + allowed + `
jobs:
  test:
` + testutil.WorkflowSteps
	errs := testutil.WorkflowDiagnostics(t, source, "policy: {disallow-suppressions: true}")
	if len(errs) != 2 || errs[0].Rule != "disallow-suppressions" || errs[1].Rule != "disallow-suppressions" || errs[0].Start.Line != 2 || errs[1].Start.Line != 3 {
		t.Fatalf("want separate prohibited directives on lines 2 and 3, got %v", errs)
	}
}

func TestInlineSuppressionBlockExpression(t *testing.T) {
	source := `on: issues
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: | # actionlint:ignore script-injection -- reviewed input
          echo '${{ github.event.issue.title }}'
`
	if got := testutil.WorkflowDiagnostics(t, source, ""); len(got) != 0 {
		t.Fatal(got)
	}
	got := testutil.WorkflowDiagnostics(t, source, "policy: {disallow-suppressions: true}")
	if len(got) != 2 || got[0].Rule != "disallow-suppressions" || got[1].Rule != "script-injection" {
		t.Fatalf("prohibited block suppression: %+v", got)
	}
}
