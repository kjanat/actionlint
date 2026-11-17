package policy_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestDependencyFindingsAndExitStatus(t *testing.T) {
	t.Parallel()
	const remote = "https://github.com/example/action/blob/abc/action.yml"
	for _, tc := range []struct {
		name, entry, path, rule, code, severity, config string
		nonBlocking                                     bool
	}{
		{"local advice", "ci.yml", "ci.yml", "script-interpolation", "", "error", "{}", false},
		{"remote advice", "ci.yml", remote, "script-interpolation", "", "error", "{}", true},
		{"remote runtime error", "ci.yml", remote, "container-runner", "", "error", "{}", false},
		{"possible runtime error", "ci.yml", remote, "container-runner", "", "warning", "{}", true},
		{"unavailable local dependency", "ci.yml", remote, "missing-local-action", "", "error", "{}", true},
		{"directly checked remote file", remote, remote, "script-interpolation", "", "error", "{}", false},
		{"all dependencies", "ci.yml", remote, "script-interpolation", "", "error", "lint: {dependencies: {fail-on-all: true}}", false},
		{"matching category", "ci.yml", remote, "script-interpolation", "", "error", "lint: {dependencies: {fail-on-categories: [correctness]}}", false},
		{"other category", "ci.yml", remote, "script-interpolation", "", "error", "lint: {dependencies: {fail-on-categories: [security]}}", true},
		{"matching rule", "ci.yml", remote, "script-interpolation", "", "error", "lint: {dependencies: {fail-on-rules: [script-interpolation]}}", false},
		{"matching code", "ci.yml", remote, "shellcheck", "SC2086", "warning", "lint: {dependencies: {fail-on-rules: [shellcheck/SC2086]}}", false},
		{"other code", "ci.yml", remote, "shellcheck", "SC2250", "info", "lint: {dependencies: {fail-on-rules: [shellcheck/SC2086]}}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rules := checking.ConfigurationRules()
			cfg, err := rules.ParseConfig([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			findings := []*finding.Finding{{Path: tc.path, Rule: tc.rule, Code: tc.code, Severity: tc.severity}}
			p := preparePolicy(rules, cfg, policy.Policy{Path: tc.entry}, nil, findings)
			got, err := publishFindings(policy.Processor{RuleSet: rules}, p, findings, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].NonBlocking != tc.nonBlocking || got[0].Severity != tc.severity {
				t.Fatalf("want non_blocking=%v and severity=%s, got %+v", tc.nonBlocking, tc.severity, got)
			}
			result := exitcode.AnalysisResult(report.Analysis{Diagnostics: []results.PublishedDiagnostic{got[0].Publish(nil)}})
			want := 1
			if tc.nonBlocking {
				want = 0
			}
			if result.ExitCode != want {
				t.Fatalf("exit code = %d, want %d", result.ExitCode, want)
			}
		})
	}
}

func TestDuplicateDependencyFindingUsesStricterCaller(t *testing.T) {
	t.Parallel()
	for _, firstNonBlocking := range []bool{false, true} {
		files := []policy.Batch{
			{InputPath: "one.yml", Policy: policy.Policy{Content: []byte("on: push")}, Findings: []*finding.Finding{{Rule: "script-interpolation", Path: "https://example.test/action.yml", Source: results.DiagnosticSource{Content: []byte("name: action")}, Message: "same finding", NonBlocking: firstNonBlocking}}},
			{InputPath: "two.yml", Policy: policy.Policy{Content: []byte("on: push")}, Findings: []*finding.Finding{{Rule: "script-interpolation", Path: "https://example.test/action.yml", Source: results.DiagnosticSource{Content: []byte("name: action")}, Message: "same finding", NonBlocking: !firstNonBlocking}}},
		}
		for i := range files {
			cfgText := "{}"
			if !files[i].Findings[0].NonBlocking {
				cfgText = "lint: {dependencies: {fail-on-all: true}}"
			}
			cfg := lifecycleRulesConfig(t, cfgText)
			files[i].Processor = policy.Processor{RuleSet: checking.ConfigurationRules()}
			files[i].Policy = preparePolicy(files[i].Processor.RuleSet, cfg, policy.Policy{Path: files[i].InputPath, Content: files[i].Policy.Content}, nil, files[i].Findings)
		}
		published, err := policy.Publish(files, nil)
		if err != nil {
			t.Fatal(err)
		}
		diagnostics := published.Diagnostics
		if len(diagnostics) != 1 || diagnostics[0].NonBlocking || published.Findings[0][0].NonBlocking {
			t.Fatalf("first non-blocking=%v: got %+v, want one blocking finding", firstNonBlocking, diagnostics)
		}
	}
}

func TestDuplicateDependencyMetadataUsesStricterCaller(t *testing.T) {
	t.Parallel()
	for _, firstNonBlocking := range []bool{false, true} {
		files := []policy.Batch{
			{InputPath: "one.yml", Policy: policy.Policy{Content: []byte("uses: example/action@v1")}},
			{InputPath: "two.yml", Policy: policy.Policy{Content: []byte("uses: example/action@v1")}},
		}
		for i := range files {
			files[i].Findings = []*finding.Finding{{
				Rule: "action", Path: "https://example.test/action.yml", Message: "invalid metadata",
				NonBlocking:      (i == 0) == firstNonBlocking,
				MetadataIdentity: &finding.MetadataIdentity{Key: finding.MetadataKey{Path: "https://example.test/action.yml", Message: "invalid metadata"}},
			}}
		}
		for i := range files {
			cfgText := "{}"
			if !files[i].Findings[0].NonBlocking {
				cfgText = "lint: {dependencies: {fail-on-all: true}}"
			}
			cfg := lifecycleRulesConfig(t, cfgText)
			files[i].Processor = policy.Processor{RuleSet: checking.ConfigurationRules()}
			files[i].Policy = preparePolicy(files[i].Processor.RuleSet, cfg, policy.Policy{Path: files[i].InputPath, Content: files[i].Policy.Content}, nil, files[i].Findings)
		}
		published, err := policy.Publish(files, nil)
		if err != nil {
			t.Fatal(err)
		}
		diagnostics := published.Diagnostics
		if len(diagnostics) != 1 || diagnostics[0].NonBlocking || published.Findings[0][0].NonBlocking {
			t.Fatalf("first non-blocking=%v: got %+v, want one blocking metadata finding", firstNonBlocking, diagnostics)
		}
	}
}
