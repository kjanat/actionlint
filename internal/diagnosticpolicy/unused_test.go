package diagnosticpolicy

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
)

func TestUnusedInlineSuppressions(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		findings     []*findingmodel.Finding
		unused       []string
	}{
		{name: "none", source: "run: echo ok # ignore[expression-reference]\n", unused: []string{"expression-reference"}},
		{name: "used", source: "run: echo ok # actionlint-ignore expression-reference\n", findings: []*findingmodel.Finding{{Rule: "expression-reference", Start: diagnostic.Position{Line: 1}}}},
		{name: "partial", source: "run: | # ignore[expression-reference shellcheck]\n  echo hi\n", findings: []*findingmodel.Finding{{Rule: "expression-reference", Start: diagnostic.Position{Line: 2}}}, unused: []string{"shellcheck"}},
		{name: "next line", source: "# ignore-next-line expression-reference\nrun: echo ok\n", unused: []string{"expression-reference"}},
		{name: "wrong declaration", source: "run: echo ok # ignore expression-reference\nnext: value\n", findings: []*findingmodel.Finding{{Rule: "expression-reference", Start: diagnostic.Position{Line: 2}}}, unused: []string{"expression-reference"}},
		{name: "plugin", source: "run: echo ok # ignore plugin-rule\n", findings: []*findingmodel.Finding{{Rule: "plugin-rule", Start: diagnostic.Position{Line: 1}}}},
		{name: "explicit audit exception", source: "run: echo ok # ignore unused-suppression expression-reference\n"},
		{name: "script text", source: "run: |\n  # ignore expression-reference\n  echo hi\n"},
		{name: "sourced script", source: "run: | # ignore shellcheck\n  . ./script.sh\n", findings: []*findingmodel.Finding{{Rule: "shellcheck", Start: diagnostic.Position{Line: 100}, Source: []byte("external"), SuppressionOrigin: &findingmodel.SuppressionOrigin{First: 2, Last: 2}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := Apply(Policy{Path: "ci.yml", ConfigPath: "ci.yml", Content: []byte(tc.source), AdditionalRules: []string{"plugin-rule"}}, tc.findings, map[string][]byte{}, nil)
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

func TestUnusedSuppressionLevels(t *testing.T) {
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
		cfg, err := repoconfig.Parse([]byte(tc.config))
		if err != nil {
			t.Fatal(err)
		}
		cfg = repoconfig.ResolveRules(repoconfig.Presets{Strict: tc.strict}, cfg)
		findings, err := Apply(Policy{Path: "ci.yml", ConfigPath: "ci.yml", Config: cfg, Content: []byte("run: echo hi # ignore expression-reference\n")}, nil, map[string][]byte{}, nil)
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
