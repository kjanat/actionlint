package capture

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/source"
)

func TestCanonicalIdentityDoesNotDeclareSourcesOrPromoteAfterSeal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root")
	var resolutions atomic.Int32
	store, err := newStoreWithResolver(t.Context(), nil, func(context.Context, string) ([]byte, error) {
		t.Error("identity-only resolution read content")
		return nil, nil
	}, func(path string) string { resolutions.Add(1); return path })
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	for range 2 {
		if identity, err := store.CanonicalIdentity(t.Context(), path); err != nil || identity != path {
			t.Fatalf("identity memo: identity=%q error=%v", identity, err)
		}
	}
	if _, state, err := store.Lookup(path); err != nil || state != Absent {
		t.Fatalf("identity-only alias became a source: state=%v error=%v", state, err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Canonical(t.Context(), path); !errors.Is(err, ErrClosed) {
		t.Fatalf("sealed identity alias was promoted: %v", err)
	}
	if _, err := store.CanonicalIdentity(t.Context(), path); err != nil {
		t.Fatalf("cached identity required new work: %v", err)
	}
	rows, err := store.Sources()
	if err != nil || len(rows) != 0 || resolutions.Load() != 1 {
		t.Fatalf("identity memo escaped to sources: rows=%+v error=%v resolutions=%d", rows, err, resolutions.Load())
	}
}

func TestCanonicalIdentityAliasRequiresExplicitDocumentPromotion(t *testing.T) {
	root := t.TempDir()
	path, alias := filepath.Join(root, "source.yml"), filepath.Join(root, "alias.yml")
	store, err := newStoreWithResolver(t.Context(), []Overlay{{Path: path, Content: []byte("captured"), Coordinates: source.YAML}}, func(context.Context, string) ([]byte, error) {
		t.Error("alias promotion reread captured bytes")
		return nil, nil
	}, func(string) string { return path })
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	if _, err := store.CanonicalIdentity(t.Context(), alias); err != nil {
		t.Fatal(err)
	}
	if _, state, err := store.Lookup(alias); err != nil || state != Absent {
		t.Fatalf("identity-only alias authorized a document: %v %v", state, err)
	}
	if _, err := store.Canonical(t.Context(), alias); err != nil {
		t.Fatal(err)
	}
	if captured, state, err := store.Lookup(alias); err != nil || state != Captured || string(captured.Bytes()) != "captured" || len(captured.Aliases()) != 2 {
		t.Fatalf("document promotion lost the existing capture: %v %v", state, err)
	}
}

func TestCanonicalIdentityAndDocumentShareOneFlight(t *testing.T) {
	for range 32 {
		path := filepath.Join(t.TempDir(), "source.yml")
		started, release := make(chan struct{}), make(chan struct{})
		var resolutions atomic.Int32
		store, err := newStoreWithResolver(t.Context(), nil, func(context.Context, string) ([]byte, error) {
			t.Error("canonicalization read content")
			return nil, nil
		}, func(path string) string { resolutions.Add(1); close(started); <-release; return path })
		if err != nil {
			t.Fatal(err)
		}
		cleanupStore(t, store)
		unblock := sync.OnceFunc(func() { close(release) })
		t.Cleanup(unblock)
		results := make(chan error, 2)
		go func() { _, err := store.CanonicalIdentity(t.Context(), path); results <- err }()
		<-started
		go func() { _, err := store.Canonical(t.Context(), path); results <- err }()
		unblock()
		for range 2 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		rows, err := store.Sources()
		if err != nil || len(rows) != 1 || rows[0].Document != path || rows[0].State != Absent || len(rows[0].Aliases) != 1 || resolutions.Load() != 1 {
			t.Fatalf("shared flight lost document promotion: rows=%+v error=%v resolutions=%d", rows, err, resolutions.Load())
		}
	}
}

func TestCanonicalIdentityCancellationStillRequiresJoinedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root")
	started, release := make(chan struct{}), make(chan struct{})
	store, err := newStoreWithResolver(t.Context(), nil, func(context.Context, string) ([]byte, error) { return nil, nil }, func(path string) string { close(started); <-release; return path })
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := store.CanonicalIdentity(ctx, path); result <- err }()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled identity waiter: %v", err)
	}
	closeCtx, stop := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer stop()
	joinErr := store.Close(closeCtx)
	unblock()
	if _, ok := errors.AsType[*JoinError](joinErr); !ok {
		t.Fatalf("identity work escaped Close: %v", joinErr)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Sources()
	if err != nil || len(rows) != 0 {
		t.Fatalf("cancelled identity-only work declared a source: %+v %v", rows, err)
	}
}
