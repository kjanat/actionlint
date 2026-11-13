package checking

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

type suppressionAnalyzer struct{ name string }

func (a suppressionAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: a.name, Category: "security", RequiredInputs: []rulemeta.InputKind{rulemeta.Scripts}}
}

func (a suppressionAnalyzer) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
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
			result, err := Check(t.Context(), Options{WorkingDir: root, Analyzers: []checkinputs.Registration{{Checker: suppressionAnalyzer{"no-sudo"}}}}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
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
	t.Parallel()
	for _, name := range []string{"no-sudo", "no-curl"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := writeSuppressionFixture(t, root, "config.yaml", "lint: {rules: {disable: ["+name+"]}}\n")
			opts := Options{WorkingDir: root, ConfigFile: path, Analyzers: []checkinputs.Registration{{Checker: suppressionAnalyzer{name}}}}
			_, err := Check(t.Context(), opts, inputmodel.Request{Kind: inputmodel.Files})
			if err != nil {
				t.Fatal(err)
			}
			opts.Analyzers = nil
			_, err = Check(t.Context(), opts, inputmodel.Request{Kind: inputmodel.Files})
			if err == nil {
				t.Fatal("an unregistered rule leaked into another session")
			}
			if !strings.Contains(err.Error(), "unknown diagnostic rule") {
				t.Fatal(err)
			}
			if _, err := ConfigurationRules().Load(filepath.Join(root, "config.yaml")); err == nil {
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
		opts := Options{WorkingDir: root, Analyzers: []checkinputs.Registration{{Checker: suppressionAnalyzer{"no-sudo"}}}}
		if _, err := Check(t.Context(), opts, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}}); err == nil || !strings.Contains(err.Error(), `unknown tool "no-sudo"`) {
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
	finding := &findingmodel.Finding{Rule: "no-sudo", Start: sourcepos.Position{Line: 4, Column: 12}, Path: path, Message: "plugin finding"}
	if got := (policy.Processor{RuleSet: ConfigurationRules()}).FilterForeignInline(path, source, []*findingmodel.Finding{finding}, nil, "no-sudo"); len(got) != 0 {
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
