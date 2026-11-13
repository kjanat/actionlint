package workflow_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	sourceref "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type literalMetadataFixture struct{ manifest actions.Manifest }

func (p literalMetadataFixture) Lookup(_ context.Context, ref actions.Reference) (actions.Manifest, error) {
	if ref == p.manifest.Selection().Request() {
		return p.manifest, nil
	}
	return actions.UnavailableManifest(availability.NotFetched()), nil
}

func TestWorkflowLiteralUsesDiagnostics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref, err := actions.ParseReference("github.com", "actions/checkout@v6")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, commit, "")
	if err != nil {
		t.Fatal(err)
	}
	route, err := actions.RefRoute("refs/tags/v6")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(ref, identity, commit, route, "test-fixture")
	if err != nil {
		t.Fatal(err)
	}
	remote, err := actions.NewManifest(selection, []byte("name: checkout\ndescription: fixture\noutputs: {commit: {description: commit}}\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
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
		{"missing action metadata", "./missing-action", "", "has no action.yml, action.yaml"},
		{"malformed metadata", "./malformed", "", "could not parse action metadata"},
		{"incomplete metadata", "./incomplete", "", "description is required"},
		{"required input", "./action", "", "missing input"},
		{"provided input", "./action", "\n        with: {token: value}", ""},
		{"invalid spec", "not-an-action", "", "invalid"},
		{"known outputs", "actions/checkout@v6", "\n      - run: echo ${{ steps.action.outputs.missing }}", `property "missing"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var plain []diagmodel.Diagnostic
			for _, uses := range []string{tc.uses, "${{ '" + tc.uses + "' }}"} {
				source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - id: action
        uses: ` + uses + tc.extra + "\npermissions: {}\n"
				request := checking.Options{WorkingDir: root, RemoteActions: literalMetadataFixture{remote}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
				var requestSources []workspace.Source
				var err error
				requestSources = []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(source), Project: workspace.Identity(root), Config: workspace.Identity(root).Config()}}
				diagnostics, err := testutil.Diagnostics(checking.Analyze(t.Context(), request, requestSources))
				if err != nil {
					t.Fatal(err)
				}
				if tc.want == "" && len(diagnostics) != 0 || tc.want != "" && (len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, tc.want)) {
					t.Fatalf("wanted %q, got %v", tc.want, diagnostics)
				}
				for _, diagnostic := range diagnostics {
					line := strings.Split(source, "\n")[diagnostic.Start.Line-1]
					if diagnostic.Snippet != line || diagnostic.End.Line != diagnostic.Start.Line || diagnostic.End.Column <= diagnostic.Start.Column || diagnostic.End.Column > len([]rune(line))+1 {
						t.Fatalf("invalid source range: %+v", diagnostic)
					}
				}
				if uses == tc.uses {
					plain = diagnostics
				} else if diff := cmp.Diff(plain, diagnostics, cmpopts.IgnoreFields(sourceref.Reference{}, "Revision"), cmpopts.IgnoreFields(diagmodel.Diagnostic{}, "End", "Snippet")); diff != "" {
					t.Fatal(diff)
				}
			}
		})
	}
}

func TestWorkflowLiteralCallInvalidSpec(t *testing.T) {
	t.Parallel()
	for _, uses := range []string{"oooops", "${{ 'oooops' }}"} {
		request := checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: ""}})}
		var requestSources []workspace.Source
		var err error
		requestSources = []workspace.Source{{Path: "workflow.yml", InputPath: "workflow.yml", Content: []byte(`on: push
jobs:
  call:
    uses: ` + uses + "\npermissions: {}\n"), Project: nil}}
		errs, err := testutil.Diagnostics(checking.Analyze(t.Context(), request, requestSources))
		if err != nil {
			t.Fatal(err)
		}
		if len(errs) != 1 || errs[0].Rule != "workflow-call" || !strings.Contains(errs[0].Message, "not following the format") {
			t.Fatalf("invalid reference %q: %v", uses, errs)
		}
	}
}
