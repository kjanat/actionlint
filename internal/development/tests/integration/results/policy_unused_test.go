package results_test

import (
	"path/filepath"
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestUnusedInlineSuppressions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		findings     []*findingmodel.Finding
		unused       []string
	}{
		{name: "none", source: "run: echo ok # ignore[expression-reference]\n", unused: []string{"expression-reference"}},
		{name: "used", source: "run: echo ok # actionlint-ignore expression-reference\n", findings: []*findingmodel.Finding{{Rule: "expression-reference", Start: sourcepos.Position{Line: 1}}}},
		{name: "partial", source: "run: | # ignore[expression-reference shellcheck]\n  echo hi\n", findings: []*findingmodel.Finding{{Rule: "expression-reference", Start: sourcepos.Position{Line: 2}}}, unused: []string{"shellcheck"}},
		{name: "next line", source: "# ignore-next-line expression-reference\nrun: echo ok\n", unused: []string{"expression-reference"}},
		{name: "wrong declaration", source: "run: echo ok # ignore expression-reference\nnext: value\n", findings: []*findingmodel.Finding{{Rule: "expression-reference", Start: sourcepos.Position{Line: 2}}}, unused: []string{"expression-reference"}},
		{name: "plugin", source: "run: echo ok # ignore plugin-rule\n", findings: []*findingmodel.Finding{{Rule: "plugin-rule", Start: sourcepos.Position{Line: 1}}}},
		{name: "explicit audit exception", source: "run: echo ok # ignore unused-suppression expression-reference\n"},
		{name: "script text", source: "run: |\n  # ignore expression-reference\n  echo hi\n"},
		{name: "sourced script", source: "run: | # ignore shellcheck\n  . ./script.sh\n", findings: []*findingmodel.Finding{{Rule: "shellcheck", Start: sourcepos.Position{Line: 100}, Source: []byte("external"), SuppressionOrigin: &findingmodel.SuppressionOrigin{First: 2, Last: 2}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings, err := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Apply(policy.Policy{Path: "ci.yml", ConfigPath: "ci.yml", Content: []byte(tc.source), AdditionalRules: []string{"plugin-rule"}}, tc.findings, map[string][]byte{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var unused []string
			for _, finding := range findings {
				if finding.Rule == "unused-suppression" {
					unused = append(unused, finding.UnusedSuppressionRule)
					if finding.Severity != "info" || finding.Start.Line != 1 || !strings.Contains(finding.Message, finding.UnusedSuppressionRule) {
						t.Fatalf("unused diagnostic: %+v", finding)
					}
				}
			}
			if strings.Join(unused, ",") != strings.Join(tc.unused, ",") {
				t.Fatalf("unused=%v, want=%v; findings=%+v", unused, tc.unused, findings)
			}
		})
	}
}

func TestLegacyAndCanonicalInlineSuppressionSelectors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ legacy, canonical string }{
		{"require-commit-hash", "unpinned-reference"},
		{"require-job-timeout", "job-timeout"},
		{"require-action-metadata", "missing-action-metadata"},
	} {
		for _, selector := range []string{tc.legacy, tc.canonical} {
			t.Run(selector, func(t *testing.T) {
				t.Parallel()
				source := []byte("run: echo ok # ignore[" + selector + "]\n")
				descriptor, found := checking.ConfigurationRules().FindRule(tc.canonical)
				if !found {
					t.Fatalf("unknown fixture rule %q", tc.canonical)
				}
				cfg, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {" + descriptor.Category + ": {" + tc.canonical + ": on}}}"))
				if err != nil {
					t.Fatal(err)
				}
				cfg, err = checking.ConfigurationRules().ResolveRules(repoconfig.Presets{}, cfg)
				if err != nil {
					t.Fatal(err)
				}
				findings, err := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Apply(policy.Policy{Path: "ci.yml", ConfigPath: "ci.yml", Config: cfg, Content: source}, []*findingmodel.Finding{{Rule: tc.canonical, Start: sourcepos.Position{Line: 1}}}, map[string][]byte{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(findings) != 0 {
					t.Fatalf("selector %q did not suppress canonical finding or was reported unused: %+v", selector, findings)
				}
			})
		}
	}
}

func TestRetiredPermissionSuppressionSelectorsAreRejected(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{"require-permissions", "explicit-permissions", "missing-workflow-permissions"} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			source := []byte("run: echo ok # ignore[" + selector + "]\n")
			findings, err := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Apply(
				policy.Policy{Path: "ci.yml", ConfigPath: "ci.yml", Content: source},
				[]*findingmodel.Finding{{Rule: "missing-permissions", Start: sourcepos.Position{Line: 1}}},
				map[string][]byte{}, nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			invalid, reported := false, false
			for _, finding := range findings {
				invalid = invalid || finding.Rule == "inline-suppression"
				reported = reported || finding.Rule == "missing-permissions"
			}
			if !invalid || !reported {
				t.Fatalf("retired selector %q suppressed the finding or was accepted: %+v", selector, findings)
			}
		})
	}
}

func TestLegacyAndCanonicalConfiguredSuppressionSelectors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "ci.yml")
	for _, selector := range []string{"require-job-timeout", "job-timeout"} {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().Parse([]byte("ignores: [{rule: " + selector + ", file: ci.yml}]"))
			if err != nil {
				t.Fatal(err)
			}
			finding := &findingmodel.Finding{Rule: "job-timeout", Path: path, Start: sourcepos.Position{Line: 1}}
			got := (policy.Processor{RuleSet: checking.ConfigurationRules()}).FilterConfigured(path, root, nil, []*findingmodel.Finding{finding}, cfg, false, func(string) repoconfig.Level { return "error" })
			if len(got) != 0 {
				t.Fatalf("configured selector %q did not suppress canonical finding: %+v", selector, got)
			}
		})
	}
}

func TestUnusedSuppressionLevels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		config string
		strict bool
		want   string
	}{
		{"{}", false, "info"},
		{"{}", true, "error"},
		{"lint: {rules: {preset: recommended}}", false, "info"},
		{"lint: {rules: {correctness: {unused-suppression: off}}}", true, ""},
		{"lint: {rules: {correctness: {unused-suppression: warn}}}", true, "warning"},
	} {
		cfg, err := checking.ConfigurationRules().Parse([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		cfg, err = checking.ConfigurationRules().ResolveRules(repoconfig.Presets{Strict: tc.strict}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		findings, err := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Apply(policy.Policy{Path: "ci.yml", ConfigPath: "ci.yml", Config: cfg, Content: []byte("run: echo hi # ignore expression-reference\n")}, nil, map[string][]byte{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, finding := range findings {
			if finding.Rule == "unused-suppression" {
				got = finding.Canonical(nil).Severity
			}
		}
		if got != tc.want {
			t.Fatalf("config=%s strict=%v severity=%s want=%s", tc.config, tc.strict, got, tc.want)
		}
	}
}
