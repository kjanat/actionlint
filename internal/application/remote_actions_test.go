package application

import (
	"actionlint.kjanat.dev/v2/actions"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

type manifestProvider func(context.Context, actions.Reference) (actions.Manifest, error)

func (p manifestProvider) Lookup(ctx context.Context, reference actions.Reference) (actions.Manifest, error) {
	return p(ctx, reference)
}

func TestRemoteMetadataIsSharedByInputsAndOutputs(t *testing.T) {
	identity, err := actions.NewIdentity("github.com", "example/action", 42, strings.Repeat("a", 40), "")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := actions.ParseReference("github.com", "example/action@v1")
	if err != nil {
		t.Fatal(err)
	}
	route, err := actions.RefRoute("refs/tags/v1")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(reference, identity, strings.Repeat("a", 40), route, "test-observation")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := actions.NewManifest(selection, []byte("name: test\ndescription: test\ninputs:\n  token: {description: token}\noutputs:\n  result: {description: result}\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	provider := manifestProvider(func(_ context.Context, reference actions.Reference) (actions.Manifest, error) {
		calls.Add(1)
		if reference.Repository() != "example/action" || reference.Ref() != "v1" {
			t.Errorf("unexpected reference: %+v", reference)
		}
		return manifest, nil
	})
	root := t.TempDir()
	result, err := Analyze(t.Context(), Options{WorkingDir: root, RemoteActions: provider}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: example/action@v1
        id: action
        with: {unlisted-input: true}
      - run: echo "$RESULT"
        env: {RESULT: '${{ steps.action.outputs.unlisted }}'}
`)}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("input and output checks fetched separately: %d", calls.Load())
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("metadata validation lost: %+v", result.Diagnostics)
	}
	seen := map[string]bool{}
	for _, diagnostic := range result.Diagnostics {
		seen[diagnostic.Rule] = true
	}
	if !seen["action"] || !seen["expression-reference"] {
		t.Fatalf("metadata did not reach both consumers: %+v", result.Diagnostics)
	}
}

func TestRemoteMetadataIsUnavailableWithoutProvider(t *testing.T) {
	root := t.TempDir()
	result, err := Analyze(t.Context(), Options{WorkingDir: root}, []workspace.Source{{Path: "ci.yml", Project: workspace.Identity(root), Content: []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
        id: checkout
        with: {unlisted-input: true}
      - run: echo "$RESULT"
        env: {RESULT: '${{ steps.checkout.outputs.unlisted }}'}
`)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unfetched metadata produced findings: %+v", result.Diagnostics)
	}
}
