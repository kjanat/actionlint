package remote_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type provider func(context.Context, actions.Reference) (actions.Manifest, error)

func (p provider) Lookup(ctx context.Context, reference actions.Reference) (actions.Manifest, error) {
	return p(ctx, reference)
}

func manifest(t *testing.T, repo, commit, directory, ref, source string) actions.Manifest {
	t.Helper()
	return manifestWithID(t, knownRepositoryID(t, 42), repo, commit, directory, ref, source)
}

func knownRepositoryID(t *testing.T, value uint64) repositoryid.ID {
	t.Helper()
	id, err := repositoryid.Known(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func manifestWithID(t *testing.T, id repositoryid.ID, repo, commit, directory, ref, source string) actions.Manifest {
	t.Helper()
	identity, err := actions.NewIdentity("github.com", repo, id, commit, directory)
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
	t.Parallel()
	for _, test := range []struct{ name, repo, path, ref string }{
		{"foreign repository", "other/action", "", "v1"},
		{"foreign path", "example/action", "sub", "v1"},
		{"different request spelling", "example/action", "", "alias"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	for _, failure := range []error{errors.New("network unavailable"), context.Canceled, context.DeadlineExceeded} {
		service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
			return actions.UnavailableManifest(availability.Failed()), failure
		}))
		metadata, err := service.Lookup(t.Context(), "example/action@v1")
		canceled := errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded)
		if metadata.Present() || canceled && !errors.Is(err, failure) || !canceled && err != nil {
			t.Fatalf("wrong unavailable outcome: %v, %v", metadata, err)
		}
	}
}

func TestProviderFailureCannotHideAnalysisCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
		cancel()
		return actions.Manifest{}, errors.New("network unavailable")
	}))
	if _, err := service.Inspect(ctx, "example/action@v1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("provider failure hid analysis cancellation: %v", err)
	}
}

func TestImmutableIdentityCannotChangeContent(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	called := false
	service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
		called = true
		return actions.UnavailableManifest(availability.NotFetched()), nil
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

func TestInvalidMetadataPreservesDiagnosticCodes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, code string }{
		{"name: [", "metadata-parse"},
		{strings.Replace(source, "name: test", "name: &name test", 1), "yaml-anchor"},
	} {
		service := remote.New(provider(func(_ context.Context, reference actions.Reference) (actions.Manifest, error) {
			return manifest(t, "example/action", strings.Repeat("a", 40), "", reference.Ref(), tc.source), nil
		}))
		result, err := service.Inspect(t.Context(), "example/action@v1")
		if err != nil {
			t.Fatal(err)
		}
		problems := result.Availability().Problems()
		if len(problems) != 1 || problems[0].Code != tc.code {
			t.Fatalf("manifest problem codes: %+v, want %s", problems, tc.code)
		}
	}
}

func TestRepositoryIDsAreComparedOnlyWhenBothAreKnown(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	for _, test := range []struct {
		name     string
		first    repositoryid.ID
		second   repositoryid.ID
		repo     string
		rejected bool
	}{
		{name: "unknown then known", second: knownRepositoryID(t, 42), repo: "example/action"},
		{name: "known then unknown", first: knownRepositoryID(t, 42), repo: "Example/Action"},
		{name: "both unknown", repo: "example/action"},
		{name: "same known", first: knownRepositoryID(t, 42), second: knownRepositoryID(t, 42), repo: "example/action"},
		{name: "different known", first: knownRepositoryID(t, 42), second: knownRepositoryID(t, 43), repo: "example/action", rejected: true},
		{name: "known ID under another name", first: knownRepositoryID(t, 42), second: knownRepositoryID(t, 42), repo: "example/renamed", rejected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := remote.New(provider(func(_ context.Context, reference actions.Reference) (actions.Manifest, error) {
				if reference.Ref() == "v1" {
					return manifestWithID(t, test.first, "example/action", commit, "", "v1", source), nil
				}
				return manifestWithID(t, test.second, test.repo, commit, "", "v2", source), nil
			}))
			if _, err := service.Lookup(t.Context(), "example/action@v1"); err != nil {
				t.Fatal(err)
			}
			_, err := service.Lookup(t.Context(), test.repo+"@v2")
			if test.rejected != (err != nil) {
				t.Fatalf("second lookup error = %v, want rejected %t", err, test.rejected)
			}
		})
	}
}
