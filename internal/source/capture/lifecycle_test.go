package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/source"
)

func TestCloseJoinsCancelledWaiterReadAndGatesPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked")
	lifetime, cancelLifetime := context.WithCancel(t.Context())
	defer cancelLifetime()
	started, release := make(chan struct{}), make(chan struct{})
	var running, reads atomic.Int32
	store, err := New(lifetime, nil, func(context.Context, string) ([]byte, error) {
		reads.Add(1)
		running.Add(1)
		defer running.Add(-1)
		close(started)
		<-release // Deliberately ignores lifetime cancellation.
		return []byte("joined"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	waiter, cancel := context.WithCancel(t.Context())
	returned := make(chan error, 1)
	go func() { _, err := store.Read(waiter, path, source.RawLF); returned <- err }()
	<-started
	cancel()
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	cancelLifetime()
	closeCtx, stopClose := context.WithTimeout(t.Context(), 10*time.Millisecond)
	err = store.Close(closeCtx)
	stopClose()
	var join *JoinError
	if !errors.As(err, &join) || !errors.Is(err, context.DeadlineExceeded) || running.Load() != 1 {
		close(release)
		t.Fatalf("unjoined Close = %v, running = %d", err, running.Load())
	}
	if _, err := store.Sources(); !errors.Is(err, ErrNotSealed) {
		close(release)
		t.Fatalf("publication allowed after join failure: %v", err)
	}
	if _, err := store.Read(t.Context(), filepath.Join(filepath.Dir(path), "new"), source.RawLF); !errors.Is(err, ErrClosed) {
		close(release)
		t.Fatalf("closed Read = %v", err)
	}
	close(release)
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if running.Load() != 0 || reads.Load() != 1 {
		t.Fatalf("work survived Close: running=%d reads=%d", running.Load(), reads.Load())
	}
	rows, err := store.Sources()
	if err != nil || len(rows) != 1 || rows[0].State != Failed || rows[0].Code != "cancelled" || !errors.Is(rows[0].Error, context.Canceled) {
		t.Fatalf("sealed sources = %#v, %v", rows, err)
	}
}

func TestCancelledCaptureCauseIsCachedAndPublished(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cancelled")
			var reads int
			store := newStore(t, nil, func(context.Context, string) ([]byte, error) {
				reads++
				return nil, errors.Join(errors.New("reader interrupted"), cause)
			})
			for range 2 {
				_, err := store.Read(t.Context(), path, source.YAML)
				failure, ok := errors.AsType[*CaptureError](err)
				if !ok || failure.Code() != "cancelled" || !strings.HasPrefix(failure.Error(), "cancelled:") || !errors.Is(err, cause) {
					t.Fatalf("cached cancellation = %v", err)
				}
			}
			if reads != 1 {
				t.Fatalf("cancelled capture was reread %d times", reads)
			}
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil || len(rows) != 1 || rows[0].Code != "cancelled" || !errors.Is(rows[0].Error, cause) {
				t.Fatalf("published cancellation = %#v, %v", rows, err)
			}
		})
	}
}

func TestConcurrentCaptureStartsAndCloseAreJoined(t *testing.T) {
	dir := t.TempDir()
	for range 32 {
		var resolutions, reads, active atomic.Int32
		store, err := newStoreWithResolver(t.Context(), nil, func(context.Context, string) ([]byte, error) {
			reads.Add(1)
			active.Add(1)
			defer active.Add(-1)
			runtime.Gosched()
			return []byte("captured"), nil
		}, func(path string) string {
			resolutions.Add(1)
			active.Add(1)
			defer active.Add(-1)
			runtime.Gosched()
			return sourceid.Document(path)
		})
		if err != nil {
			t.Fatal(err)
		}
		cleanupStore(t, store)
		readSnapshot(t, store, filepath.Join(dir, "seed"), source.YAML)
		start := make(chan struct{})
		var workers sync.WaitGroup
		for index := range 32 {
			workers.Go(func() {
				<-start
				path := filepath.Join(dir, strconv.Itoa(index))
				var err error
				if index%2 == 0 {
					_, err = store.Canonical(t.Context(), path)
				} else {
					_, err = store.Read(t.Context(), path, source.YAML)
				}
				if err != nil && !errors.Is(err, ErrClosed) {
					t.Errorf("concurrent capture = %v", err)
				}
			})
		}
		closed := make(chan error, 1)
		go func() { <-start; closed <- store.Close(t.Context()) }()
		close(start)
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
		resolvedAtClose, readAtClose := resolutions.Load(), reads.Load()
		if active.Load() != 0 {
			t.Fatal("Close returned with running metadata or content work")
		}
		workers.Wait()
		if resolutions.Load() != resolvedAtClose || reads.Load() != readAtClose {
			t.Fatal("work started after Close returned")
		}
		if _, err := store.Canonical(t.Context(), filepath.Join(dir, "after-close")); !errors.Is(err, ErrClosed) {
			t.Fatalf("new alias after Close = %v", err)
		}
		if _, err := store.Read(t.Context(), filepath.Join(dir, "after-close-read"), source.YAML); !errors.Is(err, ErrClosed) {
			t.Fatalf("new read after Close = %v", err)
		}
		rows, err := store.Sources()
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if (row.State != Absent && row.State != Captured) || len(row.Aliases) != 1 || row.Revision.Valid() != (row.State == Captured) {
				t.Fatalf("unjoined or inconsistent sealed source: %#v", row)
			}
		}
	}
}

func TestCanonicalSingleflightCancellationAndCloseJoining(t *testing.T) {
	dir := t.TempDir()
	slow, fast := filepath.Join(dir, "slow"), filepath.Join(dir, "fast")
	started, release := make(chan struct{}), make(chan struct{})
	var slowResolutions, resolutions, reads atomic.Int32
	store, err := newStoreWithResolver(t.Context(), nil, func(context.Context, string) ([]byte, error) {
		reads.Add(1)
		return []byte("unrelated"), nil
	}, func(path string) string {
		resolutions.Add(1)
		if path == slow {
			slowResolutions.Add(1)
			close(started)
			<-release
		}
		return physicalDocument(path)
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	waiter, cancel := context.WithCancel(t.Context())
	returned := make(chan error, 1)
	go func() { _, err := store.Canonical(waiter, slow); returned <- err }()
	<-started
	if _, current, err := store.Lookup(slow); current != Pending || err != nil {
		close(release)
		t.Fatalf("pending alias lookup = %q, %v", current, err)
	}
	fastCtx, stopFast := context.WithTimeout(t.Context(), time.Second)
	snapshot, err := store.Read(fastCtx, fast, source.RawLF)
	stopFast()
	if err != nil || string(snapshot.Bytes()) != "unrelated" {
		close(release)
		t.Fatalf("unrelated Read blocked on canonicalization: %v", err)
	}
	cancel()
	if err := <-returned; !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatal(err)
	}
	second, cancelSecond := context.WithTimeout(t.Context(), 10*time.Millisecond)
	_, err = store.Canonical(second, slow)
	cancelSecond()
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatal(err)
	}
	closeCtx, stopClose := context.WithCancel(t.Context())
	stopClose()
	var join *JoinError
	if err := store.Close(closeCtx); !errors.As(err, &join) {
		close(release)
		t.Fatalf("metadata work escaped Close: %v", err)
	}
	close(release)
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Canonical(t.Context(), filepath.Join(dir, "after-close")); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed Canonical = %v", err)
	}
	if slowResolutions.Load() != 1 || resolutions.Load() != 2 || reads.Load() != 1 {
		t.Fatalf("resolution/read counts = %d/%d/%d", slowResolutions.Load(), resolutions.Load(), reads.Load())
	}
	rows, err := store.Sources()
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %#v, %v", rows, err)
	}
	for _, row := range rows {
		if row.Document == physicalDocument(slow) && (row.State != Absent || !slices.Equal(row.Aliases, []string{slow})) {
			t.Fatalf("cancelled alias was not retained: %#v", row)
		}
	}
}

func TestLookupNeverStartsMetadataOrContentIO(t *testing.T) {
	dir := t.TempDir()
	good, failed, absent := filepath.Join(dir, "good"), filepath.Join(dir, "failed"), filepath.Join(dir, "absent")
	started, release := make(chan struct{}), make(chan struct{})
	wantFailure := errors.New("unreadable")
	var resolutions, reads atomic.Int32
	store, err := newStoreWithResolver(t.Context(), nil, func(_ context.Context, document string) ([]byte, error) {
		reads.Add(1)
		if document == physicalDocument(failed) {
			return nil, wantFailure
		}
		close(started)
		<-release
		return []byte("captured"), nil
	}, func(path string) string { resolutions.Add(1); return physicalDocument(path) })
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	check := func(path string, want State) error {
		t.Helper()
		beforeResolutions, beforeReads := resolutions.Load(), reads.Load()
		_, current, err := store.Lookup(path)
		if current != want || resolutions.Load() != beforeResolutions || reads.Load() != beforeReads {
			t.Fatalf("Lookup(%q) = %q, started IO", path, current)
		}
		return err
	}
	if err := check(absent, Absent); err != nil {
		t.Fatal(err)
	}
	returned := make(chan error, 1)
	go func() { _, err := store.Read(t.Context(), good, source.YAML); returned <- err }()
	<-started
	if err := check(good, Pending); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-returned; err != nil {
		t.Fatal(err)
	}
	if err := check(good, Captured); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(t.Context(), failed, source.RawLF); !errors.Is(err, wantFailure) {
		t.Fatal(err)
	}
	if err := check(failed, Failed); !errors.Is(err, wantFailure) {
		t.Fatal(err)
	}
	if resolutions.Load() != 2 || reads.Load() != 2 {
		t.Fatalf("counts = %d/%d", resolutions.Load(), reads.Load())
	}
}

func TestStdinRequiresOverlay(t *testing.T) {
	var reads atomic.Int32
	store := newStore(t, nil, func(context.Context, string) ([]byte, error) { reads.Add(1); return []byte("wrong"), nil })
	for range 2 {
		_, err := store.Read(t.Context(), "<stdin>", source.YAML)
		var failure *CaptureError
		if !errors.As(err, &failure) || failure.Document() != "<stdin>" || reads.Load() != 0 {
			t.Fatalf("stdin = %v, reads = %d", err, reads.Load())
		}
	}
}

func TestRelativeAliasesRejectedWithoutIO(t *testing.T) {
	var work atomic.Int32
	store, err := newStoreWithResolver(t.Context(), nil,
		func(context.Context, string) ([]byte, error) { work.Add(1); return nil, nil },
		func(path string) string { work.Add(1); return physicalDocument(path) })
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	for _, path := range []string{"file", "./file", "../file"} {
		if _, err := store.Canonical(t.Context(), path); err == nil {
			t.Fatalf("relative canonical accepted: %q", path)
		}
		if _, err := store.Read(t.Context(), path, source.YAML); err == nil {
			t.Fatalf("relative read accepted: %q", path)
		}
		if _, _, err := store.Lookup(path); err == nil {
			t.Fatalf("relative lookup accepted: %q", path)
		}
	}
	if work.Load() != 0 {
		t.Fatalf("relative aliases started %d tasks", work.Load())
	}
}

func TestVirtualReaderUsesHostMetadataIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "virtual", "missing", "file")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("virtual source unexpectedly exists on host: %v", err)
	}
	want := physicalDocument(path)
	store := newStore(t, nil, func(_ context.Context, document string) ([]byte, error) {
		if document != want {
			t.Errorf("virtual reader address = %q, want %q", document, want)
		}
		return []byte("exists only in reader"), nil
	})
	snapshot := readSnapshot(t, store, path, source.RawLF)
	if snapshot.Revision().Document() != want {
		t.Fatalf("virtual identity = %q", snapshot.Revision().Document())
	}
}

func TestModelConflictStickyAcrossPendingSuccessAndFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	var expected string
	for _, phase := range []string{"pending", "success", "failure"} {
		t.Run(phase, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			store := newStore(t, nil, func(context.Context, string) ([]byte, error) {
				close(started)
				<-release
				if phase == "failure" {
					return nil, errors.New("capture failed")
				}
				return []byte("abc\r\n"), nil
			})
			first := make(chan error, 1)
			go func() { _, err := store.Read(t.Context(), path, source.RawLF); first <- err }()
			<-started
			if phase != "pending" {
				close(release)
				initialErr := <-first
				if phase == "failure" {
					if captureErr, ok := errors.AsType[*CaptureError](initialErr); !ok || captureErr.Document() != physicalDocument(path) {
						t.Fatalf("initial failed capture = %v", initialErr)
					}
				} else if initialErr != nil {
					t.Fatalf("initial successful capture = %v", initialErr)
				}
			}
			_, err := store.Read(t.Context(), path, source.YAML)
			conflict := requireConflict(t, err)
			if expected == "" {
				expected = conflict.message
			} else if conflict.message != expected {
				t.Fatalf("phase changed conflict: %q != %q", conflict.message, expected)
			}
			if phase == "pending" {
				close(release)
				requireConflict(t, <-first)
			}
			_, err = store.Read(t.Context(), path, source.RawLF)
			if requireConflict(t, err).message != expected {
				t.Fatal("conflict did not remain sticky")
			}
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil || len(rows) != 1 || rows[0].State != Conflicted || rows[0].Revision.Valid() || rows[0].Coordinates.Valid() || rows[0].CoordinatesAvailable {
				t.Fatalf("quarantine exposed first-arrival evidence: %#v, %v", rows, err)
			}
		})
	}
}

func TestSealedSourcesDeterministicAcrossOrdersAndConcurrency(t *testing.T) {
	dir := t.TempDir()
	good, bad, failed := filepath.Join(dir, "good"), filepath.Join(dir, "bad"), filepath.Join(dir, "failed")
	alias := func(name string) string {
		return dir + string(filepath.Separator) + "." + string(filepath.Separator) + name
	}
	type request struct {
		path  string
		model source.Coordinates
	}
	requests := []request{{good, source.YAML}, {alias("good"), source.YAML}, {bad, source.RawLF}, {alias("bad"), source.YAML}, {failed, source.RawLF}}
	var expected []byte
	for _, concurrent := range []bool{false, true} {
		for range 2 {
			store := newStore(t, nil, func(_ context.Context, document string) ([]byte, error) {
				if filepath.Base(document) == "failed" {
					return nil, errors.New("deterministic failure")
				}
				return []byte("same physical bytes\r\n"), nil
			})
			var calls sync.WaitGroup
			for _, request := range requests {
				call := func() { _, _ = store.Read(t.Context(), request.path, request.model) }
				if concurrent {
					calls.Go(call)
				} else {
					call()
				}
			}
			calls.Wait()
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			rows, err := store.Sources()
			if err != nil || len(rows) != 3 {
				t.Fatalf("sources = %#v, %v", rows, err)
			}
			encoded, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}
			if expected == nil {
				expected = bytes.Clone(encoded)
			} else if !bytes.Equal(encoded, expected) {
				t.Fatalf("schedule changed table:\n%s\n%s", expected, encoded)
			}
			rows[0].Document = "mutated"
			rows[0].Aliases[0] = "mutated"
			fresh, err := store.Sources()
			if err != nil || fresh[0].Document == "mutated" || fresh[0].Aliases[0] == "mutated" {
				t.Fatal("caller mutated sealed table")
			}
			if _, current, err := store.Lookup(bad); current != Conflicted || requireConflict(t, err).document != sourceid.Document(physicalDocument(bad)) {
				t.Fatalf("conflict lookup = %q, %v", current, err)
			}
			slices.Reverse(requests)
		}
	}
}
