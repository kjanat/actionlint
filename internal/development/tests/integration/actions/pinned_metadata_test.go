package actions_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type pinnedMetadataFixture struct{ manifest actions.Manifest }

func (p pinnedMetadataFixture) Lookup(_ context.Context, ref actions.Reference) (actions.Manifest, error) {
	if ref == p.manifest.Selection().Request() {
		return p.manifest, nil
	}
	return actions.UnavailableManifest(availability.NotFetched()), nil
}

func TestPinnedActionInputsAndOutputs(t *testing.T) {
	t.Parallel()
	const pinned = "11d5960a326750d5838078e36cf38b85af677262"
	ref, err := actions.ParseReference("github.com", "actions/checkout@"+pinned)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, pinned, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(ref, identity, pinned, actions.ObjectRoute(), "test-fixture")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := actions.NewManifest(selection, []byte("name: checkout\ndescription: fixture\ninputs: {fetch-depth: {description: fetch depth}}\noutputs: {commit: {description: commit}}\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ref, input, output      string
		inputError, outputError bool
	}{
		{pinned, "fetch-depth", "commit", false, false},
		{pinned, "misspelled-input", "misspelled_output", true, true},
		{"0000000000000000000000000000000000000000", "misspelled-input", "misspelled_output", false, false},
	} {
		source := fmt.Sprintf(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@%s
        id: checkout
        with:
          %s: 1
      - run: echo ok
        env:
          VALUE: ${{ steps.checkout.outputs.%s }}
permissions: {}
`, tc.ref, tc.input, tc.output)
		findings := testutil.WorkflowDiagnosticsWithOptions(t, source, "tools: {shellcheck: false, ruff: false}", checking.Options{RemoteActions: pinnedMetadataFixture{manifest}})
		inputError, outputError := false, false
		for _, d := range findings {
			inputError = inputError || strings.Contains(d.Message, "misspelled-input")
			outputError = outputError || strings.Contains(d.Message, "misspelled_output")
		}
		if inputError != tc.inputError || outputError != tc.outputError {
			t.Fatalf("%+v: %+v", tc, findings)
		}
	}
}
