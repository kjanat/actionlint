package workflows

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/calls"
)

type workflowProviderFunc func(context.Context, string) (RemoteFile, error)

func (f workflowProviderFunc) LookupWorkflow(ctx context.Context, spec string) (RemoteFile, error) {
	return f(ctx, spec)
}

func TestRemoteWorkflowMetadataSharedWithoutRepositoryRoot(t *testing.T) {
	t.Parallel()
	const spec = "example/workflows/.github/workflows/build.yml@v1"
	var requests atomic.Int32
	remote := NewRemote(workflowProviderFunc(func(_ context.Context, got string) (RemoteFile, error) {
		requests.Add(1)
		return NewRemoteFile(got, strings.Repeat("a", 40), []byte("on: workflow_call\npermissions: {contents: write}\njobs: {build: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"))
	}))
	caches := []*Cache{NewCache("", nil), NewCache(t.TempDir(), nil)}
	for _, cache := range caches {
		cache.SetRemote(remote)
		if metadata, err := cache.Resolved().LookupForCall(spec); err != nil || metadata != nil || requests.Load() != 0 {
			t.Fatalf("lookup fetched metadata: %+v %v", metadata, err)
		}
	}
	var group sync.WaitGroup
	for i := range 12 {
		group.Go(func() {
			cache := caches[i%len(caches)]
			if err := cache.LoadCalls(t.Context(), []string{spec, spec}); err != nil {
				t.Error(err)
				return
			}
			metadata, err := cache.Resolved().LookupForCall(spec)
			if err != nil || metadata == nil || metadata.JobPermissions["build"]["contents"] != calls.PermissionLevelWrite {
				t.Errorf("remote permissions missing: %+v %v", metadata, err)
			}
		})
	}
	group.Wait()
	metadata, err := caches[0].Resolved().LookupForCall(spec)
	if err != nil || metadata == nil {
		t.Fatalf("metadata=%+v error=%v", metadata, err)
	}
	metadata.JobPermissions["build"]["contents"] = calls.PermissionLevelNone
	fresh, err := caches[1].Resolved().LookupForCall(spec)
	if err != nil || fresh.JobPermissions["build"]["contents"] != calls.PermissionLevelWrite || requests.Load() != 1 {
		t.Fatalf("shared mutable data or repeated requests: %+v %v requests=%d", fresh, err, requests.Load())
	}
}

func TestRemoteWorkflowUnavailableInvalidAndCanceled(t *testing.T) {
	t.Parallel()
	const spec = "example/workflows/.github/workflows/build.yml@v1"
	for _, scenario := range []string{"unavailable", "invalid", "foreign", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			remote := NewRemote(workflowProviderFunc(func(_ context.Context, request string) (RemoteFile, error) {
				if scenario == "unavailable" {
					return UnavailableRemoteFile(availability.Denied()), nil
				}
				if scenario == "canceled" {
					cancel()
				}
				if scenario == "foreign" {
					request = "other/workflows/.github/workflows/build.yml@v1"
				}
				return NewRemoteFile(request, strings.Repeat("a", 40), []byte("on: [invalid\n"))
			}))
			metadata, err := remote.Load(ctx, spec)
			if metadata != nil {
				t.Fatalf("invalid or unavailable metadata accepted: %+v", metadata)
			}
			switch scenario {
			case "unavailable":
				if err != nil {
					t.Fatal(err)
				}
			case "invalid":
				var failure metadataError
				if !errors.As(err, &failure) || failure.kind != failureParse {
					t.Fatalf("parse failure lost: %v", err)
				}
				if _, cached := remote.Lookup(spec); cached == nil || cached.Error() != err.Error() {
					t.Fatalf("cached parse failure lost: %v", cached)
				}
			case "foreign":
				if err == nil || !strings.Contains(err.Error(), "different reference") {
					t.Fatalf("foreign reference accepted: %v", err)
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
			}
		})
	}
}

func TestRemoteWorkflowReferenceValidation(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	for _, spec := range []string{"./.github/workflows/build.yml", "example/repo@v1", "example/repo/.github/workflows/../build.yml@v1", "example/repo/.github/workflows/nested/build.yml@v1", "example/repo/.github/workflows/build.yml@${{ inputs.ref }}"} {
		if IsRemoteReference(spec) {
			t.Errorf("invalid remote workflow reference accepted: %s", spec)
		}
	}
	if _, err := NewRemoteFile("example/repo/.github/workflows/build.yml@"+sha, strings.Repeat("b", 40), nil); err == nil {
		t.Fatal("provider changed a pinned commit")
	}
}

func TestRemoteWorkflowRejectsDifferentContentAtSameCommit(t *testing.T) {
	t.Parallel()
	requests := 0
	remote := NewRemote(workflowProviderFunc(func(_ context.Context, spec string) (RemoteFile, error) {
		requests++
		content := "on: workflow_call\njobs: {}\n"
		if requests > 1 {
			content += "permissions: write-all\n"
		}
		return NewRemoteFile(spec, strings.Repeat("a", 40), []byte(content))
	}))
	if _, err := remote.Load(t.Context(), "example/workflows/.github/workflows/build.yml@v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Load(t.Context(), "example/workflows/.github/workflows/build.yml@v2"); err == nil || !strings.Contains(err.Error(), "changed content") {
		t.Fatalf("conflicting immutable data accepted: %v", err)
	}
}

func TestLoadCallsFetchesRemoteWorkflowsConcurrently(t *testing.T) {
	t.Parallel()
	specs := []string{"example/first/.github/workflows/build.yml@v1", "example/second/.github/workflows/build.yml@v1", "./.github/workflows/local.yml"}
	released := make(chan struct{})
	var inFlight atomic.Int32
	cache := NewCache("", nil)
	cache.SetRemote(NewRemote(workflowProviderFunc(func(_ context.Context, spec string) (RemoteFile, error) {
		if inFlight.Add(1) == 2 {
			close(released)
		}
		select {
		case <-released:
		case <-time.After(5 * time.Second):
			return RemoteFile{}, errors.New("remote workflows were fetched one after another")
		}
		return NewRemoteFile(spec, strings.Repeat("a", 40), []byte("on: workflow_call\njobs: {}\n"))
	})))
	if err := cache.LoadCalls(t.Context(), specs); err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs[:2] {
		if metadata, err := cache.Resolved().LookupForCall(spec); err != nil || metadata == nil {
			t.Fatalf("%s=%+v,%v", spec, metadata, err)
		}
	}
}

func TestLoadCallsKeepsRemoteWorkflowErrors(t *testing.T) {
	t.Parallel()
	const spec = "example/workflows/.github/workflows/build.yml@v1"
	for _, scenario := range []string{"foreign", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var requests atomic.Int32
			cache := NewCache("", nil)
			cache.SetRemote(NewRemote(workflowProviderFunc(func(context.Context, string) (RemoteFile, error) {
				requests.Add(1)
				if scenario == "canceled" {
					cancel()
				}
				return NewRemoteFile("other/workflows/.github/workflows/build.yml@v1", strings.Repeat("a", 40), []byte("on: workflow_call\njobs: {}\n"))
			})))
			err := cache.LoadCalls(ctx, []string{spec})
			switch scenario {
			case "foreign":
				if err == nil || !strings.Contains(err.Error(), "different reference") || requests.Load() != 2 {
					t.Fatalf("error=%v requests=%d", err, requests.Load())
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) || requests.Load() != 1 {
					t.Fatalf("error=%v requests=%d", err, requests.Load())
				}
			}
		})
	}
}
