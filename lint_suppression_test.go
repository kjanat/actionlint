package actionlint

import "testing"

func TestEveryRuleIsSuppressible(t *testing.T) {
	for _, rule := range BuiltinRules() {
		t.Run(rule.Name, func(t *testing.T) {
			for _, severity := range []string{"error", "warning", "info"} {
				source := []byte("name: demo # actionlint:ignore " + rule.Name + " -- reviewed\n")
				findings := []*Error{{Line: 1, Column: 1, Kind: rule.Name, severity: severity}}
				if got := filterInlineSuppressions(source, findings, nil); len(got) != 0 {
					t.Fatal(got)
				}
			}
			cfg, err := ParseConfig([]byte("lint: {rules: {disable: [" + rule.Name + "]}}"))
			if err != nil || len(cfg.Lint.Rules.Disable) != 1 {
				t.Fatalf("%+v, %v", cfg, err)
			}
		})
	}
}

func TestGeneralInlineSuppression(t *testing.T) {
	for _, tc := range []struct{ body, config string }{
		{"    if: true # actionlint:ignore if-cond -- intentionally always runs\n", ""},
		{"    # actionlint:ignore-next-line expression -- provided by invocation\n    if: typo.value\n", ""},
		{"    if: github.ref == 'refs/heads/main' # actionlint:ignore case-insensitive-conditions -- casing intentionally irrelevant\n", "lint: {rules: {suspicious: {case-insensitive-conditions: on}}}"},
	} {
		source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n" + tc.body + "    steps:\n      - run: echo ok\n"
		if got := lintCachePolicy(t, source, tc.config); len(got) != 0 {
			t.Fatal(got)
		}
	}
	if got := lintCachePolicy(t, "jobs: [\n", "lint: {rules: {disable: [syntax-check]}}"); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestForeignInlineSuppressionSource(t *testing.T) {
	source := []byte("name: demo # actionlint:ignore shellcheck -- reviewed\n")
	finding := &Error{Line: 1, Column: 1, Kind: "shellcheck", Filepath: "local/action.yml", source: source}
	if got := filterInlineSuppressions(source, []*Error{finding}, nil); len(got) != 1 {
		t.Fatal("caller suppressed foreign finding")
	}
	if got := filterForeignInlineSuppressions(source, []*Error{finding}, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if finding.source == nil {
		t.Fatal("modified original finding")
	}
}
