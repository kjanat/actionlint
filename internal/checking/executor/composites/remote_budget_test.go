package composites

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type budgetManifestProvider struct {
	t         *testing.T
	manifests map[string]string
	requests  map[string]int
}

func (p budgetManifestProvider) Lookup(_ context.Context, ref actions.Reference) (actions.Manifest, error) {
	key := ref.Repository()
	if ref.Directory() != "" {
		key += "/" + ref.Directory()
	}
	p.requests[key]++
	body, found := p.manifests[key]
	if !found {
		p.t.Fatalf("unexpected manifest request: %s", key)
	}
	identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, strings.Repeat("a", 40), ref.Directory())
	if err != nil {
		return actions.Manifest{}, err
	}
	route, err := actions.RefRoute("refs/tags/" + ref.Ref())
	if err != nil {
		return actions.Manifest{}, err
	}
	selection, err := actions.NewSelection(ref, identity, identity.Commit(), route, "test")
	if err != nil {
		return actions.Manifest{}, err
	}
	return actions.NewManifest(selection, []byte(body))
}

func TestRemoteMetadataAcquisitionLimits(t *testing.T) {
	t.Parallel()
	const leaf = "name: leaf\ndescription: leaf\nruns: {using: composite, steps: [{shell: bash, run: echo ok}]}\n"
	for _, tc := range []struct {
		name    string
		entry   string
		steps   string
		depth   int
		want    map[string]int
		failure string
	}{
		{"wide", "[{uses: example/outer@v1}]", "[{uses: example/a@v1}, {uses: example/b@v1}, {uses: example/c@v1}, {uses: example/d@v1}]", 5, map[string]int{"example/outer": 1, "example/a": 1, "example/b": 1}, "remote action metadata lookup limit exceeded (maximum 3 per entry)"},
		{"exact limit", "[{uses: example/outer@v1}]", "[{uses: example/a@v1}, {uses: example/b@v1}]", 5, map[string]int{"example/outer": 1, "example/a": 1, "example/b": 1}, ""},
		{"repeated reference", "[{uses: example/outer@v1}]", "[{uses: example/a@v1}, {uses: example/a@v1}]", 5, map[string]int{"example/outer": 1, "example/a": 1}, ""},
		{"depth boundary", "[{uses: example/outer@v1}]", "[{uses: example/a@v1}, {uses: example/b@v1}]", 1, map[string]int{"example/outer": 1}, ""},
		{"repository relative after prefetch", "[{uses: example/outer@v1}, {uses: example/a@v1}, {uses: example/b@v1}]", "[{uses: '$/nested'}]", 5, map[string]int{"example/outer": 1, "example/a": 1, "example/b": 1}, "remote action metadata lookup limit exceeded (maximum 3 per entry)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			provider := budgetManifestProvider{t, map[string]string{
				"example/outer": "name: outer\ndescription: outer\nruns: {using: composite, steps: " + tc.steps + "}\n",
				"example/a":     leaf, "example/b": leaf, "example/c": leaf, "example/d": leaf,
			}, map[string]int{}}
			service := remote.New(provider)
			budget := invocation.Budget{MaxDepth: tc.depth, MaxCalls: 3, MaxScripts: 10}
			workflow := fmt.Sprintf("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: %s}}\n", tc.entry)
			fixture := newInvocationFixture(t, t.Context(), workflow, nil, &budget)
			fixture.options.RemoteActions = service
			fixture.options.PrepareSource = func(_ parsing.SourceSnapshot, references []string) error {
				for _, reference := range references {
					if _, err := service.Inspect(t.Context(), reference); err != nil {
						return err
					}
				}
				return nil
			}
			result, err := Workflow(fixture.options, fixture.root)
			if tc.failure == "" && err != nil || tc.failure != "" && (err == nil || !strings.Contains(err.Error(), tc.failure)) {
				t.Fatalf("analysis error = %v, want %q", err, tc.failure)
			}
			if !maps.Equal(provider.requests, tc.want) {
				t.Fatalf("manifest requests = %v, want %v", provider.requests, tc.want)
			}
			if tc.name == "depth boundary" && !result.Invocations.Incomplete() {
				t.Fatal("depth-limited calls must mark the graph incomplete")
			}
		})
	}
}
