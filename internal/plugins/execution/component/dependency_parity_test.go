package component_test

import (
	"encoding/json"
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	dependencypins "actionlint.kjanat.dev/v2/internal/development/tests/conformance/wasm/dependency-pins"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/component"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestDependencyAnalyzerNativeComponentParity(t *testing.T) {
	t.Parallel()
	plugin := loadPlugin(t, "pinned-actions", component.Options{})
	if got := plugin.Descriptor().RequiredInputs; len(got) != 1 || got[0] != rulemeta.Dependencies {
		t.Fatalf("requires=%v", got)
	}
	native := dependencypins.Checker{}
	reference := sourcepos.Reference{Document: "document", Revision: "revision", Node: "node", Address: "jobs.build.steps[0]"}
	for _, tc := range []struct {
		name, kind, uses string
		want             bool
	}{
		{"action tag", "action", "actions/checkout@v4", true},
		{"workflow branch", "workflow", "owner/repo/.github/workflows/ci.yml@main", true},
		{"action commit", "action", "owner/repo@" + strings.Repeat("a1", 20), false},
		{"workflow commit", "workflow", "owner/repo/.github/workflows/ci.yml@" + strings.Repeat("AF", 20), false},
		{"short commit", "action", "owner/repo@abcdef0", true},
		{"nonhex ref", "action", "owner/repo@" + strings.Repeat("g", 40), true},
		{"local action", "action", "./actions/setup", false},
		{"local workflow", "workflow", "./.github/workflows/ci.yml", false},
		{"docker tag", "action", "docker://alpine:3", false},
		{"docker digest", "action", "docker://alpine@sha256:abc", false},
		{"malformed", "action", "owner/repo", false},
		{"unknown kind", "future", "owner/repo@v1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := checkinputs.Input{Path: "workflow.yml", AvailableInputs: []rulemeta.InputKind{rulemeta.Dependencies}, Dependencies: []workflowfacts.Dependency{{
				Reference: reference, Uses: tc.uses, Kind: tc.kind,
				Start: sourcepos.Position{Line: 6, Column: 15}, End: sourcepos.Position{Line: 7, Column: 24},
			}}}
			// A component must not reconstruct declarations from raw source text.
			for _, content := range []string{"", "not YAML: [", "jobs: {}"} {
				input.Content = content
				want, err := native.CheckDocument(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				got, err := plugin.CheckDocument(t.Context(), input)
				if err != nil {
					t.Fatal(err)
				}
				if (len(got) == 1) != tc.want || len(got) > 1 {
					t.Fatalf("diagnostics=%+v, want finding=%v", got, tc.want)
				}
				assertDependencyFindings(t, got, want)
				if tc.want && (got[0].Reference == nil || *got[0].Reference != reference || got[0].Start != input.Dependencies[0].Start || got[0].End != input.Dependencies[0].End) {
					t.Fatalf("lost declaration provenance: %+v", got[0])
				}
			}
		})
	}
}

func TestDependencyAnalyzerSharedDeclarationFacts(t *testing.T) {
	t.Parallel()
	plugin := loadPlugin(t, "pinned-actions", component.Options{})
	const workflow = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: 'actions/checkout@v4'
  reuse:
    uses: owner/repo/.github/workflows/ci.yml@main
permissions: {}
`
	check := func(a checkinputs.Checker) []results.Diagnostic {
		t.Helper()
		result, err := checking.Analyze(t.Context(),

			checking.Options{Analyzers: []checkinputs.Registration{{Checker: a}}}, []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow)}},
		)
		if err != nil {
			t.Fatal(err)
		}
		var findings []results.Diagnostic
		for _, finding := range result.Diagnostics {
			if finding.Rule == "pinned-actions" {
				findings = append(findings, finding)
			}
		}
		return findings
	}
	got := check(plugin)
	assertDependencyFindings(t, got, check(dependencypins.Checker{}))
	if len(got) != 2 {
		t.Fatalf("diagnostics=%+v", got)
	}
	for i, want := range []struct {
		message, address string
		start, end       sourcepos.Position
	}{
		{"action must be pinned to a full commit hash", "steps[0]", sourcepos.Position{Line: 6, Column: 15}, sourcepos.Position{Line: 6, Column: 36}},
		{"reusable workflow must be pinned to a full commit hash", `jobs["reuse"]`, sourcepos.Position{Line: 8, Column: 11}, sourcepos.Position{Line: 8, Column: 51}},
	} {
		finding := got[i]
		if finding.Reference == nil || finding.Reference.Document == "" || finding.Reference.Revision == "" || finding.Reference.Node == "" || !strings.HasSuffix(finding.Reference.Address, want.address) {
			t.Fatalf("missing declaration identity: %+v", finding)
		}
		if finding.Message != want.message || finding.Start != want.start || finding.End != want.end {
			t.Fatalf("finding=%+v, want=%+v", finding, want)
		}
	}
}

func assertDependencyFindings(t *testing.T, got, want []results.Diagnostic) {
	t.Helper()
	// JSON ignores empty optional slices produced by the component boundary.
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d", len(got), len(want))
	}
	for i := range got {
		actual, err := json.Marshal(got[i])
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.Marshal(want[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(expected) {
			t.Fatalf("got %s, want %s", actual, expected)
		}
	}
}
