package remote_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	actionfixtures "actionlint.kjanat.dev/v2/internal/development/tests/integration/actions/fixtures"

	"actionlint.kjanat.dev/v2/internal/checking/expressions"
	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestRequestActionAllowsAdditionalInputsAndUndeclaredOutputs(t *testing.T) {
	fixture, err := actionfixtures.ReadManifest("request-action.yml")
	if err != nil {
		t.Fatal(err)
	}
	_, content, found := strings.Cut(string(fixture), "\n---\n")
	if !found {
		t.Fatal("request-action fixture has no provenance header")
	}
	const commit = "3d6703deef41a82916a2e5ca87096f93035cef66"
	reviewedID, err := repositoryid.Known(213507602)
	if err != nil {
		t.Fatal(err)
	}
	service := remote.New(provider(func(_ context.Context, request actions.Reference) (actions.Manifest, error) {
		identity, err := actions.NewIdentity(request.Host(), request.Repository(), reviewedID, commit, request.Directory())
		if err != nil {
			return actions.Manifest{}, err
		}
		route := actions.ObjectRoute()
		if request.Ref() != commit {
			lookup := "refs/heads/" + request.Ref()
			if request.Ref() == "test-tag" {
				lookup = "refs/tags/" + request.Ref()
			}
			route, err = actions.RefRoute(lookup)
			if err != nil {
				return actions.Manifest{}, err
			}
		}
		selection, err := actions.NewSelection(request, identity, commit, route, "")
		if err != nil {
			return actions.Manifest{}, err
		}
		return actions.NewManifest(selection, []byte(content))
	}))
	// A host-owned tag selection and raw object selection exercise the shared
	// immutable-manifest cache after the branch lookup. The synthetic test tag
	// makes no assertion about an upstream tag's target.
	for _, ref := range []string{"v3.x", "test-tag", commit} {
		t.Run(ref, func(t *testing.T) {
			spec := "octokit/request-action@" + ref
			definition, err := service.Lookup(t.Context(), spec)
			if err != nil {
				t.Fatal(err)
			}
			if !definition.AllowsAdditionalInputs() || definition.OutputsComplete() || !slices.Equal(definition.OutputNames(), []string{"data", "headers", "status"}) {
				t.Fatal("selection lost additional inputs or the declared output names")
			}
			route, exists := definition.Input("route")
			if !exists || !route.Required {
				t.Fatal("additional inputs lost the declared route requirement")
			}
			text := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + spec + `
        id: request
        with:
          route: POST /repos/{owner}/{repo}/check-runs
          owner: octokit
          repo: request-action
          name: check
          head_sha: abc
          output: body
      - run: echo result
        env:
          STATUS: ${{ steps.request.outputs.status }}
          HEADERS: ${{ steps.request.outputs.headers }}
          DATA: ${{ steps.request.outputs.data }}
          UNKNOWN: ${{ steps.request.outputs.this_output_does_not_exist }}
`
			parsed, parseErrors := workflowparser.Parse([]byte(text))
			if len(parseErrors) != 0 {
				t.Fatal(parseErrors)
			}
			view := workflowdecl.Publish(text, parsed, false, "").Bind("request-contract").Workflow()
			job, exists := view.Job("test")
			if !exists {
				t.Fatal("fixture job is missing")
			}
			checker := expressions.New(expressions.Dependencies{Action: func(spec string) (actionmeta.Definition, error) {
				return service.Lookup(t.Context(), spec)
			}}, expressions.Options{})
			if err := checker.VisitWorkflowPre(view); err != nil {
				t.Fatal(err)
			}
			if err := checker.VisitJobPre(job); err != nil {
				t.Fatal(err)
			}
			for step := range job.Steps() {
				if action, ok := step.Action(); ok {
					if findings := actioncheck.Repository(action, definition); len(findings) != 0 {
						t.Fatalf("dynamic API parameters rejected: %v", findings)
					}
				}
				if err := checker.VisitStep(step); err != nil {
					t.Fatal(err)
				}
			}
			findings := checker.Findings()
			if len(findings) != 0 {
				t.Fatalf("undeclared JavaScript action output was rejected: %v", findings)
			}
		})
	}
}
