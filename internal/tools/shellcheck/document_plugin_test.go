package shellcheck_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/shellcheck"
)

type documentGuest struct {
	inputs []analyzer.Input
	fail   bool
}

func (*documentGuest) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "shellcheck-rs", Recommended: true, Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Execution}}
}

func (g *documentGuest) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	g.inputs = append(g.inputs, input)
	start, _ := input.Scripts[0].Position(1, 6, false)
	end, _ := input.Scripts[0].Position(1, 12, true)
	reference := input.Scripts[0].Reference
	result := []diagnostic.Diagnostic{{
		Reference: &reference, Rule: "shellcheck-rs", Code: "SC2086", Message: "Quote variable", Path: input.Path, Start: start, End: end,
		Fixes: []diagnostic.Fix{{
			Description: "Quote", Applicability: "unsafe", Revisions: map[string]string{input.Path: input.Reference.Revision},
			Edits: []diagnostic.Edit{{Path: input.Path, Start: start, End: end, Replacement: `"$VALUE"`}},
		}},
	}}
	if g.fail {
		return result, errors.New("partial analysis")
	}
	return result, nil
}

func TestDocumentPluginRetainsShellcheckControlsAndFixes(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		disabled     bool
		partial      bool
		process      *processconfig.Options
	}{
		{"enabled", "{}", false, false, nil},
		{"tool disabled", "tools: {shellcheck: false}", true, false, nil},
		{"rule disabled", "lint: {rules: {external: {shellcheck: off}}}", true, false, nil},
		{"inline options", "tools: {shellcheck: {config: {disable: [SC2001], enable: [quote-safe-variables], shell: bash, extended-analysis: false}}}", false, false, nil},
		{"override disabled", "overrides: [{includes: ['ci.yml'], tools: {shellcheck: false}}]", true, false, nil},
		{"partial findings", "{}", false, true, nil},
		{"working directory", "{}", false, false, &processconfig.Options{Optional: true, WorkingDir: t.TempDir()}},
		{"wasm permitted", "{}", false, false, &processconfig.Options{Optional: true, AllowWasm: true, WorkingDir: t.TempDir()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guest := &documentGuest{fail: tc.partial}
			registration, err := (shellcheck.Options{Command: "shellcheck", Process: tc.process}).Registration().WithAnalyzer(guest)
			if err != nil {
				t.Fatal(err)
			}
			overlay, err := config.ParseOverlay("config", []byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			options := application.Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{overlay}, Tools: []tools.Registration{registration}}
			needed, err := application.RequiredTools(t.Context(), options, nil)
			if err != nil || needed["shellcheck"] {
				t.Fatalf("native dependency: %v %v", needed, err)
			}
			const workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $VALUE\n"
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Stdin, Name: "ci.yml", Reader: strings.NewReader(workflow)})
			if (err != nil) != tc.partial {
				t.Fatal(err)
			}
			if tc.disabled {
				if len(guest.inputs) != 0 || len(result.Diagnostics) != 0 {
					t.Fatalf("disabled plugin ran: %d %+v", len(guest.inputs), result.Diagnostics)
				}
				return
			}
			if len(guest.inputs) != 1 || len(result.Diagnostics) != 1 {
				t.Fatalf("duplicate/missing findings: %d %+v", len(guest.inputs), result.Diagnostics)
			}
			finding := result.Diagnostics[0]
			if finding.Rule != "shellcheck" || finding.Start.Line != 6 || finding.Start.Column != 19 || len(finding.Fixes) != 1 || finding.Fixes[0].Revisions[finding.Path] != sourceid.Digest([]byte(workflow)) {
				t.Fatalf("mapping/fix changed: %+v", finding)
			}
			if finding.Reference == nil || finding.Reference.Document == "" || finding.Reference.Node == "" || finding.Reference.Address == "$" {
				t.Fatalf("missing script identity: %+v", finding.Reference)
			}
			if tc.name == "inline options" {
				var options struct {
					Disable  []string `json:"disable"`
					Enable   []string `json:"enable"`
					Shell    string   `json:"shell"`
					Extended bool     `json:"extended-analysis"`
				}
				if err := json.Unmarshal(guest.inputs[0].Options, &options); err != nil || !slices.Equal(options.Disable, []string{"SC2001"}) || options.Shell != "bash" || options.Extended || !slices.Equal(options.Enable, []string{"quote-safe-variables"}) {
					t.Fatalf("translated options: %s %v", guest.inputs[0].Options, err)
				}
			}
		})
	}
}

func TestDocumentPluginPreservesExplicitNativeSettings(t *testing.T) {
	for _, text := range []string{
		"tools: {shellcheck: {config: .shellcheckrc}}",
		"tools: {shellcheck: {config: {disable: [SC1000-SC2000]}}}",
		"tools: {shellcheck: {backend: {process: {executable: shellcheck-custom}}}}",
	} {
		t.Run(text, func(t *testing.T) {
			overlay, err := config.ParseOverlay("config", []byte(text))
			if err != nil {
				t.Fatal(err)
			}
			registration, err := (shellcheck.Options{Command: "shellcheck"}).Registration().WithAnalyzer(&documentGuest{})
			if err != nil {
				t.Fatal(err)
			}
			needed, err := application.RequiredTools(t.Context(), application.Options{WorkingDir: t.TempDir(), ConfigOverlays: []config.Overlay{overlay}, Tools: []tools.Registration{registration}}, nil)
			if err != nil || !needed["shellcheck"] {
				t.Fatalf("explicit native settings lost: %v %v", needed, err)
			}
		})
	}
	for _, options := range []shellcheck.Options{
		{Command: "custom-shellcheck"},
		{Command: "shellcheck", Process: &processconfig.Options{Arguments: []string{"--norc"}}},
	} {
		registration, err := options.Registration().WithAnalyzer(&documentGuest{})
		if err != nil {
			t.Fatal(err)
		}
		needed, err := registration.NeedsProcess(nil, nil, false)
		if err != nil || !needed {
			t.Fatalf("explicit invocation lost: %t %v", needed, err)
		}
	}
}
