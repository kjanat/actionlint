package remote_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/actions"
	"actionlint.kjanat.dev/v2/internal/action/remote"
	"actionlint.kjanat.dev/v2/knowledge"
)

type provider func(context.Context, actions.Reference) (actions.Manifest, error)

func (p provider) Lookup(ctx context.Context, reference actions.Reference) (actions.Manifest, error) {
	return p(ctx, reference)
}

func manifest(t *testing.T, repo, commit, directory, ref, source string) actions.Manifest {
	t.Helper()
	identity, err := actions.NewIdentity("github.com", repo, 42, commit, directory)
	if err != nil {
		t.Fatal(err)
	}
	location := repo
	if directory != "" {
		location += "/" + directory
	}
	request, err := actions.ParseReference("github.com", location+"@"+ref)
	if err != nil {
		t.Fatal(err)
	}
	route, err := actions.RefRoute("refs/tags/" + ref)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := actions.NewSelection(request, identity, commit, route, "test-observation")
	if err != nil {
		t.Fatal(err)
	}
	value, err := actions.NewManifest(selection, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

const source = "name: test\ndescription: test\nruns: {using: composite, steps: []}\n"

func TestRejectForeignAndUnpinnedMetadata(t *testing.T) {
	for _, test := range []struct{ name, repo, path, ref string }{
		{"foreign repository", "other/action", "", "v1"},
		{"foreign path", "example/action", "sub", "v1"},
		{"different request spelling", "example/action", "", "alias"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := manifest(t, test.repo, strings.Repeat("a", 40), test.path, "v1", source)
			service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
				return value, nil
			}))
			if metadata, err := service.Lookup(t.Context(), "example/action@"+test.ref); err == nil || metadata.Present() {
				t.Fatalf("foreign metadata admitted: %v, %v", metadata, err)
			}
		})
	}
}

func TestUnavailableMetadataAndCancellation(t *testing.T) {
	for _, failure := range []error{errors.New("network unavailable"), context.Canceled, context.DeadlineExceeded} {
		service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
			return actions.UnavailableManifest(knowledge.Failed()), failure
		}))
		metadata, err := service.Lookup(t.Context(), "example/action@v1")
		canceled := errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded)
		if metadata.Present() || canceled && !errors.Is(err, failure) || !canceled && err != nil {
			t.Fatalf("wrong unavailable outcome: %v, %v", metadata, err)
		}
	}
}

func TestImmutableIdentityCannotChangeContent(t *testing.T) {
	for _, initial := range []string{source, "name: ["} {
		calls := 0
		service := remote.New(provider(func(_ context.Context, reference actions.Reference) (actions.Manifest, error) {
			calls++
			content := initial
			if calls > 1 {
				content = source + "outputs: {extra: {description: changed}}\n"
			}
			return manifest(t, "example/action", strings.Repeat("a", 40), "", reference.Ref(), content), nil
		}))
		if _, err := service.Lookup(t.Context(), "example/action@v1"); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Lookup(t.Context(), "example/action@alias"); err == nil {
			t.Fatal("one commit identity acquired conflicting manifests")
		}
	}
}

func TestInvalidReferencesNeverReachProvider(t *testing.T) {
	called := false
	service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
		called = true
		return actions.UnavailableManifest(knowledge.NotFetched()), nil
	}))
	for _, spec := range []string{"missing-ref", "example/action/../escape@v1", "./local"} {
		if _, err := service.Lookup(t.Context(), spec); err != nil {
			t.Fatal(err)
		}
	}
	if called {
		t.Fatal("invalid reference reached provider")
	}
}
