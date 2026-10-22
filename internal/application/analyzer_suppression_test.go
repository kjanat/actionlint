package application

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/diagnosticpolicy"
	findingmodel "actionlint.kjanat.dev/internal/finding"
)

type suppressionAnalyzer struct{ name string }

func (a suppressionAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: a.name, Requires: []analyzer.Fact{analyzer.Scripts}}
}

func (a suppressionAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	var findings []diagnostic.Diagnostic
	for _, script := range input.Scripts {
		findings = append(findings, diagnostic.Diagnostic{Path: input.Path, Rule: a.name, Start: script.Start, Message: "plugin finding"})
	}
	return findings, nil
}

func TestRegisteredAnalyzerSuppressions(t *testing.T) {
	const workflow = "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
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
		{name: "inline prohibited", directive: " # actionlint:ignore no-sudo -- reviewed", config: "policy: {disallow-suppressions: {rules: [no-sudo], report: all}}\n", want: []string{"disallow-suppressions", "no-sudo"}},
		{name: "configured prohibited", config: "ignores: [{rule: no-sudo, job: check}]\npolicy: {disallow-suppressions: {rules: [no-sudo], report: all}}\n", want: []string{"disallow-suppressions", "no-sudo"}},
		{name: "rule options prohibited", directive: " # actionlint:ignore no-sudo -- reviewed", config: "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {rules: [no-sudo], report: all}}}}}\n", want: []string{"disallow-suppressions", "no-sudo"}},
		{name: "misspelled inline", directive: " # actionlint:ignore no-sudoo -- reviewed", want: []string{"inline-suppression", "no-sudo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeSuppressionFixture(t, root, ".git/HEAD", "ref: refs/heads/master\n")
			writeSuppressionFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n"+tc.config)
			content := strings.Replace(workflow, "echo ok", "echo ok"+tc.directive, 1)
			if tc.before != "" {
				content = strings.Replace(content, "      - run:", tc.before+"      - run:", 1)
			}
			path := writeSuppressionFixture(t, root, ".github/workflows/ci.yml", content)
			result, err := Check(t.Context(), Options{WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: suppressionAnalyzer{"no-sudo"}}}}, Input{Kind: Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, finding := range result.Diagnostics {
				got = append(got, finding.Rule)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("rules=%v, want %v; findings=%+v", got, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestRegisteredAnalyzerCatalogsStayIsolated(t *testing.T) {
	for _, name := range []string{"no-sudo", "no-curl"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := writeSuppressionFixture(t, root, "config.yaml", "lint: {rules: {disable: ["+name+"]}}\n")
			opts := Options{WorkingDir: root, ConfigFile: path, Analyzers: []analyzer.Registration{{Analyzer: suppressionAnalyzer{name}}}}
			_, err := Check(t.Context(), opts, Input{Kind: Files})
			if err != nil {
				t.Fatal(err)
			}
			opts.Analyzers = nil
			_, err = Check(t.Context(), opts, Input{Kind: Files})
			if err == nil {
				t.Fatal("an unregistered rule leaked into another session")
			}
			if !strings.Contains(err.Error(), "unknown diagnostic rule") {
				t.Fatal(err)
			}
			if _, err := repoconfig.Load(filepath.Join(root, "config.yaml")); err == nil {
				t.Fatal("standalone config decoding accepted an unregistered rule")
			}
		})
	}
}

func TestRegisteredAnalyzerForeignSuppression(t *testing.T) {
	const path = "actions/check/action.yml"
	source := []byte("runs:\n  using: composite\n  steps:\n    - run: echo ok # actionlint:ignore no-sudo -- reviewed\n      shell: bash\n")
	finding := &findingmodel.Finding{Rule: "no-sudo", Start: diagnostic.Position{Line: 4, Column: 12}, Path: path, Message: "plugin finding"}
	if got := diagnosticpolicy.FilterForeignInline(path, source, []*findingmodel.Finding{finding}, nil, "no-sudo"); len(got) != 0 {
		t.Fatalf("registered plugin finding escaped composite suppression: %+v", got)
	}
}

func writeSuppressionFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
