package lookup_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/lookup"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type localLookup struct {
	metadata *actionmeta.Metadata
	err      error
	calls    int
}

func (l *localLookup) FindMetadata(string) (*actionmeta.Metadata, bool, error) {
	l.calls++
	return l.metadata, false, l.err
}

type provider func(context.Context, actions.Reference) (actions.Manifest, error)

func (p provider) Lookup(ctx context.Context, ref actions.Reference) (actions.Manifest, error) {
	return p(ctx, ref)
}

func TestWorkspaceAndRepositoryDefinitionsAgree(t *testing.T) {
	t.Parallel()
	const source = "name: test\ndescription: test\ninputs: {token: {description: token, required: true}}\noutputs: {value: {description: result, value: ok}}\nruns: {using: composite, steps: []}\n"
	metadata, err := actionmeta.Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	local := &localLookup{metadata: metadata}
	requests := 0
	service := remote.New(provider(func(_ context.Context, ref actions.Reference) (actions.Manifest, error) {
		requests++
		commit := strings.Repeat("a", 40)
		identity, err := actions.NewIdentity(ref.Host(), ref.Repository(), repositoryid.ID{}, commit, ref.Directory())
		if err != nil {
			t.Fatal(err)
		}
		route, err := actions.RefRoute("refs/tags/" + ref.Ref())
		if err != nil {
			t.Fatal(err)
		}
		selection, err := actions.NewSelection(ref, identity, commit, route, "test-observation")
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := actions.NewManifest(selection, []byte(source))
		return manifest, err
	}))
	resolver := lookup.New(t.Context(), local, service.Resolved())
	const repositoryReference = "example/action@v1"
	missing, err := resolver.Lookup(repositoryReference)
	if err != nil || missing.Present() || requests != 0 || local.calls != 0 {
		t.Fatalf("unprepared reference fetched or acquired a definition: %+v %v; requests=%d local=%d", missing, err, requests, local.calls)
	}
	if _, err := service.Inspect(t.Context(), repositoryReference); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"./action", "$/action", repositoryReference} {
		definition, err := resolver.Lookup(reference)
		if err != nil || !definition.Present() {
			t.Fatalf("%s: missing definition: %v", reference, err)
		}
		input, exists := definition.Input("token")
		if definition.Using() != "composite" || !definition.OutputsComplete() || !slices.Equal(definition.OutputNames(), []string{"value"}) || !exists || !input.Required {
			t.Fatalf("%s: inconsistent inputs, outputs or runtime: %+v", reference, definition)
		}
	}
	if requests != 1 || local.calls != 2 {
		t.Fatalf("lookup routing: provider=%d local=%d", requests, local.calls)
	}
	definition, err := resolver.Lookup("./action")
	if err != nil {
		t.Fatal(err)
	}
	delete(metadata.Outputs, "value")
	metadata.Inputs["token"].Required = false
	input, _ := definition.Input("token")
	if !input.Required || !slices.Equal(definition.OutputNames(), []string{"value"}) {
		t.Fatal("local metadata mutation changed a published definition")
	}
}

func TestUnavailableDefinitionsAndLookupFailures(t *testing.T) {
	t.Parallel()
	resolver := lookup.New(t.Context(), nil, nil)
	for _, reference := range []string{"./action", "$/action", "example/action@v1", "docker://alpine:latest"} {
		definition, err := resolver.Lookup(reference)
		if err != nil || definition.Present() {
			t.Fatalf("%s: unavailable reference acquired a definition: %+v %v", reference, definition, err)
		}
	}
	cause := errors.New("cannot read manifest")
	failed := lookup.New(t.Context(), &localLookup{err: cause}, nil)
	if definition, err := failed.Lookup("./action"); definition.Present() || !errors.Is(err, cause) {
		t.Fatalf("read failure lost: %+v %v", definition, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	canceled := lookup.New(ctx, nil, nil)
	if definition, err := canceled.Lookup("example/action@v1"); definition.Present() || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %+v %v", definition, err)
	}
}
