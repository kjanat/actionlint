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
			cfg, err := rules.Parse([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			got, err := (policy.Processor{RuleSet: rules}).Apply(policy.Policy{Path: tc.entry, Config: cfg}, []*finding.Finding{{Path: tc.path, Rule: tc.rule, Code: tc.code, Severity: tc.severity}}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].NonBlocking != tc.nonBlocking || got[0].Severity != tc.severity {
				t.Fatalf("want non_blocking=%v and severity=%s, got %+v", tc.nonBlocking, tc.severity, got)
			}
			result := exitcode.AnalysisResult(report.Analysis{Diagnostics: []results.Diagnostic{got[0].Canonical(nil)}})
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
		files := []policy.File{
			{InputPath: "one.yml", Content: []byte("on: push"), Findings: []*finding.Finding{{Rule: "script-interpolation", Path: "https://example.test/action.yml", Source: []byte("name: action"), Message: "same finding", NonBlocking: firstNonBlocking}}},
			{InputPath: "two.yml", Content: []byte("on: push"), Findings: []*finding.Finding{{Rule: "script-interpolation", Path: "https://example.test/action.yml", Source: []byte("name: action"), Message: "same finding", NonBlocking: !firstNonBlocking}}},
		}
		diagnostics, sources := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect(files)
		if len(diagnostics) != 1 || len(sources) != 1 || diagnostics[0].NonBlocking || files[0].Findings[0].NonBlocking {
			t.Fatalf("first non-blocking=%v: got %+v and %d sources, want one blocking finding", firstNonBlocking, diagnostics, len(sources))
		}
	}
}

func TestDuplicateDependencyMetadataUsesStricterCaller(t *testing.T) {
	t.Parallel()
	for _, firstNonBlocking := range []bool{false, true} {
		files := []policy.File{
			{InputPath: "one.yml", Content: []byte("uses: example/action@v1")},
			{InputPath: "two.yml", Content: []byte("uses: example/action@v1")},
		}
		for i := range files {
			files[i].Findings = []*finding.Finding{{
				Rule: "action", Path: files[i].InputPath, Message: "invalid metadata",
				NonBlocking:      (i == 0) == firstNonBlocking,
				MetadataIdentity: &finding.MetadataIdentity{Key: finding.MetadataKey{Path: "https://example.test/action.yml", Message: "invalid metadata"}},
			}}
		}
		diagnostics, sources := (policy.Processor{RuleSet: checking.ConfigurationRules()}).Collect(files)
		if len(diagnostics) != 1 || len(sources) != 1 || diagnostics[0].NonBlocking || files[0].Findings[0].NonBlocking {
			t.Fatalf("first non-blocking=%v: got %+v and %d sources, want one blocking metadata finding", firstNonBlocking, diagnostics, len(sources))
		}
	}
}
