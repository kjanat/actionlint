package policy_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestSelectedRemoteOriginWinsRegardlessOfBatchOrder(t *testing.T) {
	t.Parallel()
	const remote = "https://github.com/example/action/blob/abc/action.yml"
	for _, entries := range [][]string{{"ci.yml", remote}, {remote, "ci.yml"}} {
		var batches []policy.Batch
		for _, entry := range entries {
			findings := []*finding.Finding{{Rule: "script-interpolation", Path: remote, Message: "same finding", Source: results.DiagnosticSource{Content: []byte("name: action")}}}
			rules := checking.ConfigurationRules()
			cfg := lifecycleRulesConfig(t, "{}")
			p := preparePolicy(rules, cfg, policy.DocumentInputs{Path: entry}, nil, findings)
			batches = append(batches, policy.Batch{InputPath: entry, Policy: p, Processor: policy.Processor{RuleSet: rules}, Findings: findings})
		}
		got, err := policy.Publish(batches, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Diagnostics) != 1 || got.Diagnostics[0].Origin != results.ProjectOrigin || got.Diagnostics[0].NonBlocking {
			t.Fatalf("entries=%v: selected remote did not win: %+v", entries, got.Diagnostics)
		}
	}
}

func TestSelectedRemoteAliasHasProjectOrigin(t *testing.T) {
	t.Parallel()
	const selected = "https://github.com/example/action/blob/v1/action.yml"
	const canonical = "https://github.com/example/action/blob/abc/action.yml"
	rules := checking.ConfigurationRules()
	findings := []*finding.Finding{{Rule: "script-interpolation", Path: canonical, Message: "same finding"}}
	p := preparePolicy(rules, lifecycleRulesConfig(t, "{}"), policy.DocumentInputs{Path: selected}, nil, findings)
	processor := policy.Processor{RuleSet: rules, CanonicalPaths: map[string]string{selected: canonical, canonical: canonical}}
	got, err := publishFindings(processor, p, findings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Origin != results.ProjectOrigin || got[0].NonBlocking {
		t.Fatalf("selected remote alias became a dependency: %+v", got)
	}
}

func TestDependencyPinDeduplicationRequiresEqualContentsAndSemantics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, secondContent, secondMessage string
		want                               int
	}{
		{"identical", "name: action", "same finding", 1},
		{"different contents", "name: other", "same finding", 2},
		{"different finding", "name: action", "different finding", 2},
		{"unknown contents", "", "same finding", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var batches []policy.Batch
			for i, pin := range []string{"abc", "def", "abc"} {
				content, message := []byte("name: action"), "same finding"
				if i == 1 {
					content, message = []byte(tc.secondContent), tc.secondMessage
					if tc.secondContent == "" {
						content = nil
					}
				}
				path := "https://github.com/example/action/blob/" + pin + "/action.yml"
				ref := sourcetext.NewReferenceIndex(path, content).Ref("", "action", "", "/")
				findings := []*finding.Finding{{
					Rule: "script-interpolation", Path: path, Message: message, Reference: &ref,
					Source: results.DiagnosticSource{Content: content},
					Fixes:  []results.Fix{{Description: "Use an environment variable", Revisions: map[string]string{path: ref.Revision}, Edits: []results.Edit{{Path: path, Replacement: "$INPUT"}}}},
				}}
				rules := checking.ConfigurationRules()
				p := preparePolicy(rules, lifecycleRulesConfig(t, "{}"), policy.DocumentInputs{Path: "ci.yml", Content: []byte("on: push")}, nil, findings)
				batches = append(batches, policy.Batch{InputPath: "ci.yml", Policy: p, Processor: policy.Processor{RuleSet: rules}, Findings: findings})
			}
			got, err := policy.Publish(batches, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Diagnostics) != tc.want {
				t.Fatalf("got %d findings; want %d: %+v", len(got.Diagnostics), tc.want, got.Diagnostics)
			}
			if tc.want == 1 && !slices.ContainsFunc(got.Diagnostics[0].Related, func(r results.Related) bool {
				return r.Message == results.IdenticalDependencyLocation && r.Path == "https://github.com/example/action/blob/def/action.yml"
			}) {
				t.Fatalf("deduplicated finding lost its second pin: %+v", got.Diagnostics[0])
			}
		})
	}
}

func TestGeneratedSuppressionOriginAndDependencyUnusedSuppression(t *testing.T) {
	t.Parallel()
	const remote = "https://github.com/example/action/blob/abc/action.yml"
	for _, entry := range []string{"ci.yml", remote} {
		rules := checking.ConfigurationRules()
		cfg := lifecycleRulesConfig(t, "lint: {rules: {correctness: {unused-suppression: warn}}}")
		sources := map[string][]byte{remote: []byte("# actionlint:ignore-next-line script-interpolation\nname: action\n")}
		p := preparePolicy(rules, cfg, policy.DocumentInputs{Path: entry}, map[string]*configuration.Config{remote: cfg}, nil)
		got, err := publishFindings(policy.Processor{RuleSet: rules}, p, nil, sources)
		if err != nil {
			t.Fatal(err)
		}
		if entry == remote {
			if len(got) != 1 || got[0].Rule != "unused-suppression" || got[0].Origin != results.ProjectOrigin {
				t.Fatalf("selected remote missing its generated project finding: %+v", got)
			}
		} else if len(got) != 0 {
			t.Fatalf("consumer received a dependency's unused suppression: %+v", got)
		}
	}
}

func TestGeneratedDependencySuppressionFindingHasOrigin(t *testing.T) {
	t.Parallel()
	const remote = "https://github.com/example/action/blob/abc/action.yml"
	rules := checking.ConfigurationRules()
	cfg := lifecycleRulesConfig(t, "{}")
	sources := map[string][]byte{remote: []byte("# actionlint:ignore script-interpolation\nname: action\n")}
	p := preparePolicy(rules, cfg, policy.DocumentInputs{Path: "ci.yml", Content: []byte("on: push")}, map[string]*configuration.Config{remote: cfg}, nil)
	got, err := publishFindings(policy.Processor{RuleSet: rules}, p, nil, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rule != "inline-suppression" || got[0].Origin != results.DependencyOrigin {
		t.Fatalf("generated dependency finding has no origin: %+v", got)
	}
}

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
			findings := []*finding.Finding{{Path: tc.path, Rule: tc.rule, Code: tc.code, Severity: severity.Severity(tc.severity)}}
			p := preparePolicy(rules, cfg, policy.DocumentInputs{Path: tc.entry}, nil, findings)
			got, err := publishFindings(policy.Processor{RuleSet: rules}, p, findings, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].NonBlocking != tc.nonBlocking || string(got[0].Severity) != tc.severity {
				t.Fatalf("want non_blocking=%v and severity=%s, got %+v", tc.nonBlocking, tc.severity, got)
			}
			origin := results.ProjectOrigin
			if tc.path == remote && tc.entry != remote {
				origin = results.DependencyOrigin
			}
			if got[0].Origin != origin {
				t.Fatalf("origin = %q; want %q", got[0].Origin, origin)
			}
			result := exitcode.AnalysisResult(report.Analysis{Diagnostics: []results.PublishedDiagnostic{got[0].Publish(nil)}}, false)
			want := 1
			if tc.nonBlocking || tc.severity != "error" {
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
			{InputPath: "one.yml", Policy: policy.DocumentInputs{Content: []byte("on: push")}, Findings: []*finding.Finding{{Rule: "script-interpolation", Path: "https://example.test/action.yml", Source: results.DiagnosticSource{Content: []byte("name: action")}, Message: "same finding", NonBlocking: firstNonBlocking}}},
			{InputPath: "two.yml", Policy: policy.DocumentInputs{Content: []byte("on: push")}, Findings: []*finding.Finding{{Rule: "script-interpolation", Path: "https://example.test/action.yml", Source: results.DiagnosticSource{Content: []byte("name: action")}, Message: "same finding", NonBlocking: !firstNonBlocking}}},
		}
		for i := range files {
			cfgText := "{}"
			if !files[i].Findings[0].NonBlocking {
				cfgText = "lint: {dependencies: {fail-on-all: true}}"
			}
			cfg := lifecycleRulesConfig(t, cfgText)
			files[i].Processor = policy.Processor{RuleSet: checking.ConfigurationRules()}
			files[i].Policy = preparePolicy(files[i].Processor.RuleSet, cfg, policy.DocumentInputs{Path: files[i].InputPath, Content: files[i].Policy.Content}, nil, files[i].Findings)
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
			{InputPath: "one.yml", Policy: policy.DocumentInputs{Content: []byte("uses: example/action@v1")}},
			{InputPath: "two.yml", Policy: policy.DocumentInputs{Content: []byte("uses: example/action@v1")}},
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
			files[i].Policy = preparePolicy(files[i].Processor.RuleSet, cfg, policy.DocumentInputs{Path: files[i].InputPath, Content: files[i].Policy.Content}, nil, files[i].Findings)
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
