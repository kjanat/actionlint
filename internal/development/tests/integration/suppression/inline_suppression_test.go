package suppression_test

import (
	"strings"
	"testing"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
)

func TestInlineSuppressionMultilinePlainComments(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"name: one\n  two",
		"name: !!str\n  example",
		"name: &title\n  example",
		"name: &title !!str\n  example",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			for _, tc := range []struct {
				name, rule, config string
				kinds              []string
			}{
				{"unsupported", "unknown-rule", "", []string{"inline-suppression"}},
				{"prohibited", "cache-operation", "policy: {disallow-suppressions: true}", []string{"unused-suppression", "forbidden-suppression"}},
			} {
				t.Run(value+"/"+ending+"/"+tc.name, func(t *testing.T) {
					t.Parallel()
					source := "on: push\n" + value + " # actionlint:ignore " + tc.rule + " -- reviewed\n"
					if strings.Contains(value, "&title") {
						source += "run-name: *title\n"
					}
					source += `jobs:
  test:
` + testutil.WorkflowSteps + "permissions: {}\n"
					errs := testutil.WorkflowDiagnostics(t, strings.ReplaceAll(source, "\n", ending), tc.config)
					if len(errs) != len(tc.kinds) {
						t.Fatalf("want %v on line 3; got %v", tc.kinds, errs)
					}
					for i, err := range errs {
						if err.Rule != tc.kinds[i] || err.Start.Line != 3 {
							t.Errorf("diagnostic %d: want %s on line 3; got %s on line %d", i, tc.kinds[i], err.Rule, err.Start.Line)
						}
					}
				})
			}
		}
	}
}

func TestInlineSuppressionScalarPrefixes(t *testing.T) {
	t.Parallel()
	const invalid = "# actionlint:ignore unknown-rule -- reviewed"
	const allowed = "# actionlint:ignore cache-operation -- reviewed"
	for _, tc := range []struct {
		name, value      string
		line, unusedLine int
	}{
		{"literal", "name: !!str\n  | " + invalid + "\n    title\n", 3, 0},
		{"folded", "name: !!str\n  > " + invalid + "\n    title\n", 3, 0},
		{"literal indicator", "name: !!str\n  |2- " + invalid + "\n    title\n", 3, 0},
		{"property only", "name: !!str " + invalid + "\n  title\n", 2, 0},
		{"property and value", "name: !!str " + invalid + "\n  title " + allowed + "\n", 2, 3},
		{"property and block header", "name: !!str " + allowed + "\n  | " + invalid + "\n    title\n", 3, 2},
		{"standalone before scalar", `name: !!str
  # actionlint:ignore-next-line unknown-rule -- reviewed
  title
`, 3, 0},
		{"block body is text", "name: !!str\n  | " + invalid + "\n    title " + allowed + "\n", 3, 0},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+"/"+ending, func(t *testing.T) {
				t.Parallel()
				source := "on: push\n" + tc.value + `jobs:
  test:
` + testutil.WorkflowSteps + "\npermissions: {}\n"
				errs := testutil.WorkflowDiagnostics(t, strings.ReplaceAll(source, "\n", ending), "")
				want := map[string]int{"inline-suppression": tc.line}
				if tc.unusedLine != 0 {
					want["unused-suppression"] = tc.unusedLine
				}
				if len(errs) != len(want) {
					t.Fatalf("want %v, got %v", want, errs)
				}
				for _, err := range errs {
					if line, exists := want[err.Rule]; !exists || err.Start.Line != line {
						t.Errorf("want %v; got %s on line %d", want, err.Rule, err.Start.Line)
					}
					delete(want, err.Rule)
				}
			})
		}
	}
	// Identical comments on the property and value are separate directives.
	source := "on: push\nname: !!str " + allowed + "\n  title " + allowed + `
jobs:
  test:
` + testutil.WorkflowSteps + "\npermissions: {}\n"
	errs := testutil.WorkflowDiagnostics(t, source, "policy: {disallow-suppressions: true}")
	if len(errs) != 4 {
		t.Fatalf("want unused and prohibited diagnostics on lines 2 and 3, got %v", errs)
	}
	for i, rule := range []string{"unused-suppression", "forbidden-suppression", "unused-suppression", "forbidden-suppression"} {
		if errs[i].Rule != rule || errs[i].Start.Line != 2+i/2 {
			t.Errorf("diagnostic %d: want %s on line %d; got %s on line %d", i, rule, 2+i/2, errs[i].Rule, errs[i].Start.Line)
		}
	}
}

func TestInlineSuppressionBlockExpression(t *testing.T) {
	t.Parallel()
	source := `on: issues
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: | # actionlint:ignore script-injection -- reviewed input
          echo '${{ github.event.issue.title }}'
permissions: {}
`
	if got := testutil.WorkflowDiagnostics(t, source, ""); len(got) != 0 {
		t.Fatal(got)
	}
	got := testutil.WorkflowDiagnostics(t, source, "policy: {disallow-suppressions: true}")
	if len(got) != 2 || got[0].Rule != "forbidden-suppression" || got[1].Rule != "script-injection" {
		t.Fatalf("prohibited block suppression: %+v", got)
	}
}
