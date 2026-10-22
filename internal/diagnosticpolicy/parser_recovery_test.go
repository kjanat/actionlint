package diagnosticpolicy_test

import (
	"strings"
	"testing"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"

	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	"actionlint.kjanat.dev/internal/suppression"
)

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
			_, findings := parseSuppressionSource(source)
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
	_, findings := parseSuppressionSource(source)
	got := diagnosticpolicy.FilterInline(source, findings, nil)
	if len(got) != 2 || got[1].Rule != "inline-suppression" || !strings.Contains(got[1].Message, "reason") {
		t.Fatalf("explicit empty reason was accepted: %+v", got)
	}
	cfg, err := repoconfig.Parse([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	source = []byte("jobs: [ # actionlint:ignore syntax-check -- reviewed")
	_, findings = parseSuppressionSource(source)
	if got := diagnosticpolicy.FilterInline(source, findings, cfg.Policy.DisallowSuppressions); len(got) != 2 || got[1].Rule != "disallow-suppressions" {
		t.Fatalf("malformed YAML bypassed suppression policy: %+v", got)
	}
	source = []byte("name: test # actionlint:ignore syntax-check -- reviewed\njobs: [")
	_, findings = parseSuppressionSource(source)
	if got := diagnosticpolicy.FilterInline(source, findings, nil); len(got) != 1 || got[0] != findings[0] {
		t.Fatalf("unrelated declaration suppressed syntax error: %+v", got)
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
			_, findings := parseSuppressionSource(source)
			if len(findings) != 1 || findings[0].Rule != "syntax-check" {
				t.Fatalf("invalid boundary fixture: %+v", findings)
			}
			if got := diagnosticpolicy.FilterInline(source, findings, nil); len(got) != 1 || got[0] != findings[0] {
				t.Fatalf("unrelated comment suppressed malformed header: %+v", got)
			}
		}
		for _, reason := range []string{" --", " -- intentional"} {
			source := []byte(strings.ReplaceAll("run: # actionlint:ignore syntax-check"+reason+"\n  !!str\n  | bad\n    echo ok", "\n", ending))
			_, findings := parseSuppressionSource(source)
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

func parseSuppressionSource(source []byte) (*workflowlang.Workflow, []*findingmodel.Finding) {
	workflow, diagnostics := workflowlang.ParseDocument(source)
	findings := make([]*findingmodel.Finding, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		findings = append(findings, findingmodel.FromDiagnostic(diagnostic))
	}
	return workflow, findings
}
