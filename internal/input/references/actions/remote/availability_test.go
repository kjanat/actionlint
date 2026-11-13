package remote_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func TestMetadataAvailabilityPreservesMissingAndInvalid(t *testing.T) {
	t.Parallel()
	disabled := remote.New(nil)
	missing, err := disabled.Inspect(t.Context(), "example/action@v1")
	if err != nil || missing.Availability().State() != actions.MetadataUnavailable || missing.Availability().Reason() != availability.LookupDisabledReason {
		t.Fatalf("disabled = %v, %v", missing.Availability(), err)
	}
	service := remote.New(provider(func(_ context.Context, reference actions.Reference) (actions.Manifest, error) {
		return manifest(t, "example/action", strings.Repeat("a", 40), "", reference.Ref(), "name: ["), nil
	}))
	invalid, err := service.Inspect(t.Context(), "example/action@v1")
	if err != nil || invalid.Availability().State() != actions.MetadataInvalid || invalid.Definition().Present() || len(invalid.Availability().Problems()) == 0 {
		t.Fatalf("invalid = %v, %v", invalid.Availability(), err)
	}
	problems := invalid.Availability().Problems()
	problems[0].Message = "mutated"
	if invalid.Availability().Problems()[0].Message == "mutated" {
		t.Fatal("invalid diagnostics alias caller storage")
	}
	coverage := service.Coverage()
	if len(coverage) != 1 || coverage[0].Reference != "example/action@v1" || coverage[0].Availability.State() != actions.MetadataInvalid {
		t.Fatalf("coverage = %v", coverage)
	}
}

func TestProviderUnavailabilityReachesCoverage(t *testing.T) {
	t.Parallel()
	for _, missing := range []availability.Unavailability{
		availability.NotFetched(), availability.Offline(), availability.LookupDisabled(),
		availability.Denied(), availability.Failed(), availability.NotFound(),
		availability.Unsupported(), availability.CacheMiss(), availability.Inaccessible(), availability.Limit(),
		availability.UnsupportedHost(), availability.ResolverNotConfigured(),
		availability.UnsupportedObjectFormat(), availability.InvalidResolverResult(),
	} {
		t.Run(string(missing.Reason()), func(t *testing.T) {
			t.Parallel()
			service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
				return actions.UnavailableManifest(missing), nil
			}))
			result, err := service.Inspect(t.Context(), "example/action@v1")
			if err != nil || result.Availability().State() != actions.MetadataUnavailable || result.Availability().Reason() != missing.Reason() {
				t.Fatalf("result=%v error=%v, want %s", result.Availability(), err, missing.Reason())
			}
			coverage := service.Coverage()
			if len(coverage) != 1 || coverage[0].Report().Reason != missing.Reason() {
				t.Fatalf("coverage=%v, want %s", coverage, missing.Reason())
			}
		})
	}
}

func TestEmptyAvailableManifestIsInvalidMetadata(t *testing.T) {
	t.Parallel()
	service := remote.New(provider(func(_ context.Context, reference actions.Reference) (actions.Manifest, error) {
		return manifest(t, "example/action", strings.Repeat("a", 40), "", reference.Ref(), ""), nil
	}))
	result, err := service.Inspect(t.Context(), "example/action@v1")
	if err != nil || result.Availability().State() != actions.MetadataInvalid {
		t.Fatalf("empty metadata = %v, %v", result.Availability(), err)
	}
}

type checkedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *checkedContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}

func TestWaitingMetadataLookupCanCancel(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	service := remote.New(provider(func(context.Context, actions.Reference) (actions.Manifest, error) {
		close(started)
		<-release
		return actions.UnavailableManifest(availability.Offline()), nil
	}))
	first := make(chan struct{})
	go func() { defer close(first); _, _ = service.Inspect(t.Context(), "example/action@v1") }()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	checked := &checkedContext{Context: ctx, checked: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := service.Inspect(checked, "example/action@v1"); done <- err }()
	<-checked.checked
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("cancel=%v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("canceled waiter blocked on acquisition")
	}
	close(release)
	<-first
}
