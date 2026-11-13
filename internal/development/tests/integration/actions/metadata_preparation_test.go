package actions_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type countedMetadataProvider struct {
	pinnedMetadataFixture
	requests int
}

func (p *countedMetadataProvider) Lookup(ctx context.Context, reference actions.Reference) (actions.Manifest, error) {
	p.requests++
	return p.pinnedMetadataFixture.Lookup(ctx, reference)
}

func TestMetadataPreparationForRepeatedAndStandaloneCompositeActions(t *testing.T) {
	t.Parallel()
	const commit = "11d5960a326750d5838078e36cf38b85af677262"
	reference, err := actions.ParseReference("github.com", "example/action@"+commit)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := actions.NewIdentity(reference.Host(), reference.Repository(), repositoryid.ID{}, commit, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(reference, identity, commit, actions.ObjectRoute(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := actions.NewManifest(selection, []byte("name: fixture\ndescription: fixture\ninputs: {accepted: {description: accepted}}\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	root, _ := testutil.ExecutableFixture(t, "shellcheck", "ruff")
	path := fixturefiles.Write(t, root, "inner/action.yml", "name: inner\ndescription: inner\nruns:\n  using: composite\n  steps:\n    - uses: example/action@"+commit+"\n      with: {misspelled-input: value}\n")
	provider := &countedMetadataProvider{pinnedMetadataFixture: pinnedMetadataFixture{manifest}}
	result := testutil.Composite(t, root, "- uses: $/inner\n- uses: $/inner", checking.Options{RemoteActions: provider})
	findings := 0
	for _, finding := range result.Diagnostics {
		if strings.Contains(finding.Message, "misspelled-input") {
			findings++
		}
	}
	// Both invocations share one source declaration and one acquired manifest.
	if provider.requests != 1 || findings != 1 {
		t.Fatalf("repeated calls: requests=%d input findings=%d diagnostics=%+v", provider.requests, findings, result.Diagnostics)
	}
	provider.requests = 0
	result, err = checking.Check(t.Context(), checking.Options{WorkingDir: root, RemoteActions: provider}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	findings = 0
	for _, finding := range result.Diagnostics {
		if strings.Contains(finding.Message, "misspelled-input") {
			findings++
		}
	}
	if provider.requests != 1 || findings != 1 {
		t.Fatalf("standalone action: requests=%d input findings=%d diagnostics=%+v", provider.requests, findings, result.Diagnostics)
	}
}
