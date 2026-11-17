package checking

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

type suppressionAnalyzer struct{ name string }

func (a suppressionAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: a.name, Category: "security", RequiredInputs: []rule.InputKind{rule.Scripts}}
}

func (a suppressionAnalyzer) AnalyzeDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, script := range input.Scripts {
		findings = append(findings, results.Diagnostic{Path: input.Path, Rule: a.name, Start: script.Start, Message: "plugin finding"})
	}
	return findings, nil
}

func TestRegisteredAnalyzerSuppressions(t *testing.T) {
	t.Parallel()
	const workflow = "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\npermissions: {}\n"
	for _, tc := range []struct {
		name, directive, before, config string
		want                            []string
	}{
		{name: "inline", directive: " # actionlint:ignore no-sudo -- reviewed"},
		{name: "next line", before: "      # actionlint:ignore-next-line no-sudo -- reviewed\n"},
		{name: "configured", config: "ignores: [{rule: no-sudo, job: check}]\n"},
		{name: "disabled", config: "lint: {rules: {disable: [no-sudo]}}\n"},
		{name: "lint disabled", config: "lint: {enabled: false}\n"},
		{name: "override disabled", config: "overrides: [{includes: ['**'], lint: {rules: {disable: [no-sudo]}}}]\n"},
		{name: "inline prohibited", directive: " # actionlint:ignore no-sudo -- reviewed", config: "policy: {disallow-suppressions: {rules: [no-sudo], report: all}}\n", want: []string{"forbidden-suppression", "no-sudo"}},
		{name: "configured prohibited", config: "ignores: [{rule: no-sudo, job: check}]\npolicy: {disallow-suppressions: {rules: [no-sudo], report: all}}\n", want: []string{"forbidden-suppression", "no-sudo"}},
		{name: "rule options prohibited", directive: " # actionlint:ignore no-sudo -- reviewed", config: "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {rules: [no-sudo], report: all}}}}}\n", want: []string{"forbidden-suppression", "no-sudo"}},
		{name: "misspelled inline", directive: " # actionlint:ignore no-sudoo -- reviewed", want: []string{"inline-suppression", "no-sudo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSuppressionFixture(t, root, ".git/HEAD", "ref: refs/heads/master\n")
			writeSuppressionFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n"+tc.config)
			content := strings.Replace(workflow, "echo ok", "echo ok"+tc.directive, 1)
			if tc.before != "" {
				content = strings.Replace(content, "      - run:", tc.before+"      - run:", 1)
			}
			path := writeSuppressionFixture(t, root, ".github/workflows/ci.yml", content)
			result, err := Check(t.Context(), Options{WorkingDir: root, Analyzers: []results.AnalyzerRegistration{{Analyzer: suppressionAnalyzer{"no-sudo"}}}}, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range result.Diagnostics {
				got = append(got, item.Rule)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("rules=%v, want %v; findings=%+v", got, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestRegisteredAnalyzerCatalogsStayIsolated(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"no-sudo", "no-curl"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := writeSuppressionFixture(t, root, "config.yaml", "lint: {rules: {disable: ["+name+"]}}\n")
			opts := Options{WorkingDir: root, ConfigFile: path, Analyzers: []results.AnalyzerRegistration{{Analyzer: suppressionAnalyzer{name}}}}
			_, err := Check(t.Context(), opts, checkrequest.Request{Kind: checkrequest.Files})
			if err != nil {
				t.Fatal(err)
			}
			opts.Analyzers = nil
			_, err = Check(t.Context(), opts, checkrequest.Request{Kind: checkrequest.Files})
			if err == nil {
				t.Fatal("an unregistered rule leaked into another session")
			}
			if !strings.Contains(err.Error(), "unknown diagnostic rule") {
				t.Fatal(err)
			}
			if _, err := ConfigurationRules().LoadConfig(filepath.Join(root, "config.yaml")); err == nil {
				t.Fatal("standalone config decoding accepted an unregistered rule")
			}
		})
	}
}

// Configuring a document analyzer under tools.<name> produces an error.
// Only tool providers can be configured in that section.
func TestAnalyzerNameIsNotATool(t *testing.T) {
	t.Parallel()
	for _, config := range []string{
		"tools: {shellcheck: false, ruff: false, no-sudo: false}\n",
		"tools: {shellcheck: false, ruff: false}\noverrides: [{includes: ['**'], tools: {no-sudo: {config: {level: 2}}}}]\n",
	} {
		root := t.TempDir()
		writeSuppressionFixture(t, root, ".git/HEAD", "ref: refs/heads/master\n")
		writeSuppressionFixture(t, root, ".github/actionlint.yaml", config)
		path := writeSuppressionFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")
		opts := Options{WorkingDir: root, Analyzers: []results.AnalyzerRegistration{{Analyzer: suppressionAnalyzer{"no-sudo"}}}}
		if _, err := Check(t.Context(), opts, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{path}}); err == nil || !strings.Contains(err.Error(), `unknown tool "no-sudo"`) {
			t.Fatalf("%s: analyzer accepted as a tool: %v", config, err)
		}
		if _, err := RequiredTools(t.Context(), opts, []string{path}); err == nil || !strings.Contains(err.Error(), `unknown tool "no-sudo"`) {
			t.Fatalf("%s: required tools accepted an analyzer as a tool: %v", config, err)
		}
	}
}

func TestRegisteredAnalyzerForeignSuppression(t *testing.T) {
	t.Parallel()
	const path = "actions/check/action.yml"
	source := []byte("runs:\n  using: composite\n  steps:\n    - run: echo ok # actionlint:ignore no-sudo -- reviewed\n      shell: bash\n")
	item := &finding.Finding{Rule: "no-sudo", Start: sourcetext.Position{Line: 4, Column: 12}, Path: path, Message: "plugin finding"}
	rules := ConfigurationRules()
	registry := rulebinding.NewRegistry(rules)
	scope := registry.Prepare(nil, nil, nil)
	item.Binding = scope.Rule(item.Rule).ID()
	p := policy.DocumentInputs{Path: "workflow.yml", ConfigPath: "workflow.yml", Scope: scope, Bindings: registry.Seal(), SourceBindings: map[string]*rulebinding.Scope{path: scope}, AdditionalRules: []string{"no-sudo"}}
	_, fixture := capturefixture.NewCapturedSources(t, t.TempDir())
	snapshot, err := fixture.Capture(path, source, sourcetext.YAML)
	if err != nil {
		t.Fatal(err)
	}
	revision := snapshot.Revision()
	refs := map[string]facts.SourceIndex{path: {Document: sourcetext.Reference{Document: revision.Document(), Revision: revision.Digest()}}}
	got, err := policy.Publish([]policy.Batch{{Processor: policy.Processor{RuleSet: rules}, Policy: p, InputPath: "workflow.yml", Findings: []*finding.Finding{item}, Sources: map[string][]byte{path: source}, References: refs}}, nil)
	if err != nil || len(got.Findings[0]) != 0 {
		t.Fatalf("registered plugin finding escaped composite suppression: %+v %v", got.Findings, err)
	}
}

func writeSuppressionFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
