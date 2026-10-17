package engine

import (
	"fmt"
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/internal/config"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/suppression"

	"github.com/google/go-cmp/cmp"
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
` + cachePolicySteps
					errs := lintCachePolicy(t, strings.ReplaceAll(source, "\n", ending), tc.config)
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
` + cachePolicySteps
				errs := lintCachePolicy(t, strings.ReplaceAll(source, "\n", ending), "")
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
` + cachePolicySteps
	errs := lintCachePolicy(t, source, "policy: {disallow-suppressions: true}")
	if len(errs) != 2 || errs[0].Rule != "disallow-suppressions" || errs[1].Rule != "disallow-suppressions" || errs[0].Start.Line != 2 || errs[1].Start.Line != 3 {
		t.Fatalf("want separate prohibited directives on lines 2 and 3, got %v", errs)
	}
}

func TestInlineSuppressionRegistry(t *testing.T) {
	for _, name := range diagnosticpolicy.SuppressionRules() {
		t.Run(name, func(t *testing.T) {
			cfg, err := repoconfig.Parse(fmt.Appendf(nil, "policy: {disallow-suppressions: {rules: [%s]}}", name))
			if err != nil {
				t.Fatal(err)
			}
			source := []byte("first: value\nsecond: value\nthird: value\n# actionlint:ignore-next-line " + name + " -- reviewed\nfifth: value\n")
			finding := &findingmodel.Finding{Start: diagmodel.Position{Line: 5}, Rule: name}
			if got := diagnosticpolicy.FilterInline(source, []*findingmodel.Finding{finding}, nil); len(got) != 0 {
				t.Fatalf("registered rule was not suppressed: %v", got)
			}
			got := diagnosticpolicy.FilterInline(source, []*findingmodel.Finding{finding}, cfg.Policy.DisallowSuppressions)
			if len(got) != 2 || got[0] != finding || got[1].Rule != "disallow-suppressions" || got[1].Start.Line != 4 {
				t.Fatalf("restriction did not retain finding and report comment: %v", got)
			}
		})
	}
}

func TestInlineSuppressionBlockBody(t *testing.T) {
	for _, header := range []string{
		"run: | # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"run: >- # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"run: |2+ # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed\nrun: |",
		"run: &script | # actionlint:ignore expression-reference,shellcheck -- reviewed",
		"run: !!str\n  | # actionlint:ignore expression-reference,shellcheck -- reviewed",
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run: !!str
  |`,
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run: &script
  |2-`,
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run: &script !!str
  >-`,
		`run: # actionlint:ignore expression-reference,shellcheck -- reviewed
  !!str
  |`,
		`run:
  !!str
  &script # actionlint:ignore expression-reference,shellcheck -- reviewed
  |`,
		`# actionlint:ignore-next-line expression-reference,shellcheck -- reviewed
run:
  &script
  !!str
  >-`,
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(header+ending, func(t *testing.T) {
				start := strings.Count(header, "\n") + 1
				source := strings.ReplaceAll(header+`
    echo first
    echo second
next: value
`, "\n", ending)
				findings := []*findingmodel.Finding{
					{Start: diagmodel.Position{Line: start + 1}, Rule: "expression-reference"},
					{Start: diagmodel.Position{Line: start + 2}, Rule: "shellcheck"},
					{Start: diagmodel.Position{Line: start + 2}, Rule: "action"},
					{Start: diagmodel.Position{Line: start + 3}, Rule: "expression-reference"},
				}
				got := diagnosticpolicy.FilterInline([]byte(source), findings, nil)
				if diff := cmp.Diff(findings[2:], got, cmp.AllowUnexported(findingmodel.Finding{})); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestInlineSuppressionSplitPrefixIsolation(t *testing.T) {
	for _, header := range []string{
		`env: {SAFE: value} # actionlint:ignore expression-reference,shellcheck -- unrelated
run:
  !!str
  |`,
		`run:
  !!str
  |
    # actionlint:ignore expression-reference,shellcheck -- script text`,
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(header+"\n    echo body\n", "\n", ending)
			line := strings.Count(header, "\n") + 2
			findings := []*findingmodel.Finding{{Start: diagmodel.Position{Line: line}, Rule: "expression-reference"}, {Start: diagmodel.Position{Line: line}, Rule: "shellcheck"}}
			if got := diagnosticpolicy.FilterInline([]byte(source), findings, nil); len(got) != 2 {
				t.Fatalf("unrelated prefix suppressed body: %q: %+v", source, got)
			}
		}
	}
}

func TestInlineSuppressionMalformedYAML(t *testing.T) {
	for _, source := range []string{
		"jobs: [ # actionlint:ignore syntax-check -- intentional",
		"# actionlint:ignore-next-line syntax-check -- intentional\njobs: [",
		`run: # actionlint:ignore syntax-check -- intentional
  !!str
  | bad
    echo ok`,
		`run:
  !!str # actionlint:ignore syntax-check -- intentional
  &script
  | bad
    echo ok`,
		`# actionlint:ignore-next-line syntax-check -- intentional
run:
  &script
  !!str
  > bad
    echo ok`,
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := []byte(strings.ReplaceAll(source, "\n", ending))
			_, findings := Parse(source)
			if len(findings) != 1 || findings[0].Rule != "syntax-check" {
				t.Fatalf("malformed fixture findings: %+v", findings)
			}
			if got := diagnosticpolicy.FilterInline(source, findings, nil); len(got) != 0 {
				t.Fatalf("syntax finding was not suppressed: %+v", got)
			}
		}
	}
	for _, source := range []string{
		"name: '# actionlint:ignore syntax-check -- text'\njobs: [",
		"name: \"# actionlint:ignore syntax-check -- text\"\njobs: [",
		`name: "first
  # actionlint:ignore syntax-check -- text
  last"
jobs: [`,
		`name: 'first
  # actionlint:ignore syntax-check -- text
  last'
jobs: [`,
		`name: 'it''s
  # actionlint:ignore syntax-check -- text
  last'
jobs: [`,
		`name: "escaped \"
  # actionlint:ignore syntax-check -- text
  last"
jobs: [`,
		`run: |
  # actionlint:ignore syntax-check -- script
jobs: [`,
		`run: >-
  print('''
  # actionlint:ignore syntax-check -- script
  ''')
jobs: [`,
		`run: !!str
  |
    # actionlint:ignore syntax-check -- script
jobs: [`,
		`run: !!str

  |2
  # actionlint:ignore syntax-check -- script
jobs: [`,
		`run: !!str
# header comment
  |2
  # actionlint:ignore syntax-check -- script
jobs: [`,
		`steps:
  - run: &script |2
      # actionlint:ignore syntax-check -- script
jobs: [`,
		"jobs: [\n# actionlint:ignore-next-line syntax-check -- no declaration",
		`jobs: [
# actionlint:ignore-next-line syntax-check -- no declaration
`,
		`jobs: [
# actionlint:ignore-next-line syntax-check -- blank

  value`,
		`jobs: [
# actionlint:ignore-next-line syntax-check -- comment
# another comment
  value`,
	} {
		if got, invalid := suppression.Parse([]byte(source), diagnosticpolicy.SuppressionRules()); len(got) != 0 || len(invalid) != 0 {
			t.Fatalf("scalar content became a directive in %q: %+v", source, got)
		}
	}
	source := []byte("jobs: [ # actionlint:ignore syntax-check --")
	_, findings := Parse(source)
	got := diagnosticpolicy.FilterInline(source, findings, nil)
	if len(got) != 2 || got[1].Rule != "inline-suppression" || !strings.Contains(got[1].Message, "reason") {
		t.Fatalf("explicit empty reason was accepted: %+v", got)
	}
	cfg, err := repoconfig.Parse([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	source = []byte("jobs: [ # actionlint:ignore syntax-check -- reviewed")
	_, findings = Parse(source)
	if got := diagnosticpolicy.FilterInline(source, findings, cfg.Policy.DisallowSuppressions); len(got) != 2 || got[1].Rule != "disallow-suppressions" {
		t.Fatalf("malformed YAML bypassed suppression policy: %+v", got)
	}
	source = []byte("name: test # actionlint:ignore syntax-check -- reviewed\njobs: [")
	_, findings = Parse(source)
	if got := diagnosticpolicy.FilterInline(source, findings, nil); len(got) != 1 || got[0] != findings[0] {
		t.Fatalf("unrelated declaration suppressed syntax error: %+v", got)
	}
}

func TestInlineSuppressionScriptTextIsNotDirective(t *testing.T) {
	source := []byte(`run: |
  text = '''
  # actionlint:ignore-next-line expression-reference -- sample text
  ${{ github.event.issue.title }}
  '''
`)
	finding := &findingmodel.Finding{Start: diagmodel.Position{Line: 4}, Rule: "expression-reference"}
	got := diagnosticpolicy.FilterInline(source, []*findingmodel.Finding{finding}, nil)
	if len(got) != 1 || got[0] != finding {
		t.Fatalf("script text changed findings: %+v", got)
	}
}

func TestRecoveredSplitPrefixSuppressionBoundaries(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, prefix := range []string{
			`name: # actionlint:ignore syntax-check -- unrelated
run:
  !!str
  | bad
    echo ok`,
			`name: # actionlint:ignore syntax-check -- unrelated
!!str run:
  !!str
  | bad
    echo ok`,
			`name: # actionlint:ignore syntax-check -- unrelated
&key run:
  !!str
  | bad
    echo ok`,
			`name: '# actionlint:ignore syntax-check -- quoted'
run:
  !!str
  | bad
    echo ok`,
			`run:
  !!str
  |
    # actionlint:ignore syntax-check -- script
    echo ok
jobs: [`,
		} {
			source := []byte(strings.ReplaceAll(prefix, "\n", ending))
			_, findings := Parse(source)
			if len(findings) != 1 || findings[0].Rule != "syntax-check" {
				t.Fatalf("invalid boundary fixture: %+v", findings)
			}
			if got := diagnosticpolicy.FilterInline(source, findings, nil); len(got) != 1 || got[0] != findings[0] {
				t.Fatalf("unrelated comment suppressed malformed header: %+v", got)
			}
		}
		for _, reason := range []string{" --", " -- intentional"} {
			source := []byte(strings.ReplaceAll("run: # actionlint:ignore syntax-check"+reason+"\n  !!str\n  | bad\n    echo ok", "\n", ending))
			_, findings := Parse(source)
			cfg, err := repoconfig.Parse([]byte("policy: {disallow-suppressions: true}"))
			if err != nil {
				t.Fatal(err)
			}
			want := "disallow-suppressions"
			if reason == " --" {
				want = "inline-suppression"
			}
			if got := diagnosticpolicy.FilterInline(source, findings, cfg.Policy.DisallowSuppressions); len(got) != 2 || got[1].Rule != want {
				t.Fatalf("recovered prefix bypassed reason or policy: %+v", got)
			}
		}
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
	if got := lintCachePolicy(t, source, ""); len(got) != 0 {
		t.Fatal(got)
	}
	got := lintCachePolicy(t, source, "policy: {disallow-suppressions: true}")
	if len(got) != 2 || got[0].Rule != "disallow-suppressions" || got[1].Rule != "script-injection" {
		t.Fatalf("prohibited block suppression: %+v", got)
	}
}
