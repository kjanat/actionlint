package remote_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func TestResolvedActionsCannotFetch(t *testing.T) {
	t.Parallel()
	requests := 0
	service := remote.New(provider(func(_ context.Context, reference actions.Reference) (actions.Manifest, error) {
		requests++
		return manifest(t, reference.Repository(), strings.Repeat("a", 40), reference.Directory(), reference.Ref(), source), nil
	}))
	resolved := service.Resolved()
	const reference = "example/action@v1"
	if got := resolved.Inspect(reference); got.Availability().Reason() != availability.NotFetchedReason || got.Definition().Present() {
		t.Fatalf("unprepared reference = %+v", got)
	}
	if requests != 0 {
		t.Fatal("reading an unprepared reference started a provider request")
	}
	if _, err := service.Inspect(t.Context(), reference); err != nil {
		t.Fatal(err)
	}
	if got := resolved.Inspect(reference); got.Availability().State() != actions.MetadataAvailable || !resolved.Lookup(reference).Present() {
		t.Fatalf("acquired reference was not published to the reader: %+v", got)
	}
	if got := resolved.Inspect("other/action@v1"); got.Availability().Reason() != availability.NotFetchedReason {
		t.Fatalf("unacquired reference = %+v", got)
	}
	if requests != 1 {
		t.Fatalf("read-only lookups started additional requests: %d", requests)
	}
}

func TestNilResolvedActionsReportDisabledLookup(t *testing.T) {
	t.Parallel()
	var resolved *remote.ResolvedActions
	if got := resolved.Inspect("example/action@v1"); got.Availability().Reason() != availability.LookupDisabledReason || resolved.Lookup("example/action@v1").Present() {
		t.Fatalf("disabled lookup = %+v", got)
	}
}
