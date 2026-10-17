package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
	sourceref "actionlint.kjanat.dev/source"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestWorkflowLiteralUsesDiagnostics(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "action"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `name: test
description: test
inputs:
  token:
    description: token
    required: true
runs:
  using: composite
  steps: [{run: echo ok, shell: bash}]
`
	if err := os.WriteFile(filepath.Join(root, "action", "action.yml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "malformed"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "malformed", "action.yml"), []byte("runs: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "incomplete"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "incomplete", "action.yml"), []byte("name: incomplete\nruns: {using: composite, steps: [{run: echo ok, shell: bash}]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, uses, extra, want string }{
		{"missing action metadata", "./missing-action", "", "has no action.yml or action.yaml"},
		{"malformed metadata", "./malformed", "", "could not parse action metadata"},
		{"incomplete metadata", "./incomplete", "", "description is required"},
		{"required input", "./action", "", "missing input"},
		{"provided input", "./action", "\n        with: {token: value}", ""},
		{"invalid spec", "not-an-action", "", "invalid"},
		{"known outputs", "actions/checkout@v6", "\n      - run: echo ${{ steps.action.outputs.missing }}", `property "missing"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plain []*findingmodel.Finding
			for _, uses := range []string{tc.uses, "${{ '" + tc.uses + "' }}"} {
				source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - id: action
        uses: ` + uses + tc.extra + "\n"
				request := Request{WorkingDir: root, Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
				var err error

				request.Sources = []SourceUnit{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: workspace.Identity(root), Config: (workspace.Identity(root)).Config()}}
				diagnostics, err := testFindings(runTestAnalysis(t.Context(), request))
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "" && len(diagnostics) != 0 || tc.want != "" && (len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, tc.want)) {
					t.Fatalf("wanted %q, got %v", tc.want, diagnostics)
				}
				if uses == tc.uses {
					plain = diagnostics
				} else if diff := cmp.Diff(plain, diagnostics, cmpopts.IgnoreFields(sourceref.Reference{}, "Revision"), cmp.AllowUnexported(findingmodel.Finding{}, findingmodel.MetadataIdentity{}, findingmodel.MetadataKey{})); diff != "" {
					t.Fatal(diff)
				}
			}
		})
	}
}

func TestWorkflowLiteralCallInvalidSpec(t *testing.T) {
	for _, uses := range []string{"oooops", "${{ 'oooops' }}"} {
		request := Request{Tools: (adapters.Options{ShellCheck: ""}).Registrations()}
		var err error

		request.Sources = []SourceUnit{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(`on: push
jobs:
  call:
    uses: ` + uses + "\n"), Project: nil}}
		errs, err := testFindings(runTestAnalysis(t.Context(), request))
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 1 || errs[0].Rule != "workflow-call" || !strings.Contains(errs[0].Message, "not following the format") {
			t.Fatalf("invalid reference %q: %v", uses, errs)
		}
	}
}
