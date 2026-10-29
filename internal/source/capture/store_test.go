package capture

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/source"
)

func newStore(t *testing.T, overlays []Overlay, reader Reader) *Store {
	t.Helper()
	store, err := New(t.Context(), overlays, reader)
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	return store
}

func cleanupStore(t *testing.T, store *Store) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
}

func readSnapshot(t *testing.T, store *Store, path string, model source.Coordinates) Snapshot {
	t.Helper()
	snapshot, err := store.Read(t.Context(), path, model)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

type conflictDetails struct {
	document string
	message  string
}

func requireConflict(t *testing.T, err error) conflictDetails {
	t.Helper()
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Code() != "source-revision-conflict" {
		t.Fatalf("expected source-revision-conflict, got %v", err)
	}
	return conflictDetails{document: conflict.Document(), message: conflict.Error()}
}

func TestConcurrentAliasesShareOneReadAndCacheSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yml")
	alias := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "workflow.yml"
	var reads atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	store := newStore(t, nil, func(_ context.Context, document string) ([]byte, error) {
		if document != sourceid.Document(fsread.PhysicalPath(path)) {
			t.Errorf("reader address = %q", document)
		}
		reads.Add(1)
		close(started)
		<-release
		return []byte("name: captured\r\n"), nil
	})
	const callers = 16
	var wait sync.WaitGroup
	for index := range callers {
		wait.Go(func() {
			address := path
			if index%2 != 0 {
				address = alias
			}
			snapshot, err := store.Read(t.Context(), address, source.YAML)
			if err != nil || !snapshot.Valid() {
				t.Errorf("read = %v, %v", snapshot.Valid(), err)
			}
		})
	}
	<-started
	close(release)
	wait.Wait()
	final := readSnapshot(t, store, alias, source.YAML)
	expected := []string{path, alias}
	slices.Sort(expected)
	if reads.Load() != 1 || !slices.Equal(final.Aliases(), expected) {
		t.Fatalf("reads = %d, aliases = %q", reads.Load(), final.Aliases())
	}
}

func TestFailuresAreCached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yml")
	want := &os.PathError{Op: "read", Path: path, Err: os.ErrNotExist}
	var reads int
	store := newStore(t, nil, func(context.Context, string) ([]byte, error) {
		reads++
		return nil, want
	})
	for range 2 {
		if _, err := store.Read(t.Context(), path, source.YAML); !errors.Is(err, want) {
			t.Fatal(err)
		} else {
			var failure *CaptureError
			if !errors.As(err, &failure) || failure.Code() != "capture-failed" || failure.Document() != sourceid.Document(fsread.PhysicalPath(path)) {
				t.Fatalf("failure = %v", err)
			}
			var original *os.PathError
			if !errors.As(err, &original) || original != want || !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("capture failure lost its cause: %v", err)
			}
		}
	}
	if reads != 1 {
		t.Fatalf("reads = %d", reads)
	}
}

func TestCancelledWaiterDoesNotPoisonSharedCapture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared")
	started, release := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	store := newStore(t, nil, func(ctx context.Context, _ string) ([]byte, error) {
		reads.Add(1)
		close(started)
		select {
		case <-release:
			return []byte("winner"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	waiter, cancel := context.WithCancel(t.Context())
	failed := make(chan error, 1)
	go func() { _, err := store.Read(waiter, path, source.RawLF); failed <- err }()
	<-started
	cancel()
	if err := <-failed; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if got := readSnapshot(t, store, path, source.RawLF); string(got.Bytes()) != "winner" || reads.Load() != 1 {
		t.Fatalf("content = %q, reads = %d", got.Bytes(), reads.Load())
	}
}

func TestAlreadyCancelledCallerStartsNoRead(t *testing.T) {
	var reads atomic.Int32
	store := newStore(t, nil, func(context.Context, string) ([]byte, error) {
		reads.Add(1)
		return nil, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Read(ctx, filepath.Join(t.TempDir(), "unused"), source.YAML); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if reads.Load() != 0 {
		t.Fatalf("reads = %d", reads.Load())
	}
}

func TestRootCancellationStopsAndCachesBackingRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared")
	root, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	var reads atomic.Int32
	store, err := New(root, nil, func(ctx context.Context, _ string) ([]byte, error) {
		reads.Add(1)
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanupStore(t, store)
	finished := make(chan error, 1)
	go func() { _, err := store.Read(t.Context(), path, source.RawLF); finished <- err }()
	<-started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := store.Read(t.Context(), path, source.RawLF); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if reads.Load() != 1 {
		t.Fatalf("reads = %d", reads.Load())
	}
}

func TestOverlayAndSnapshotMutationIsolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	alias := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "file"
	content := []byte("\xef\xbb\xbfabc\r\n")
	expected := bytes.Clone(content)
	store := newStore(t, []Overlay{{path, content, source.RawLF}}, func(context.Context, string) ([]byte, error) {
		t.Error("overlay performed a backing read")
		return nil, errors.New("unexpected overlay backing read")
	})
	content[0] = 'X'
	first := readSnapshot(t, store, path, source.RawLF)
	first.Bytes()[0] = 'Y'
	first.Aliases()[0] = "mutated"
	second := readSnapshot(t, store, alias, source.RawLF)
	if !bytes.Equal(first.Bytes(), expected) || !bytes.Equal(second.Bytes(), expected) {
		t.Fatal("captured bytes changed")
	}
	if !slices.Equal(first.Aliases(), []string{path}) || len(second.Aliases()) != 2 {
		t.Fatalf("alias views changed: %q, %q", first.Aliases(), second.Aliases())
	}
	if err := store.Verify(first, expected); err != nil {
		t.Fatal(err)
	}
	if first.Revision().Digest() != sourceid.Digest(expected) {
		t.Fatal("digest normalized physical bytes")
	}
}

func TestOverlayConflictsDeterministicAndIsolated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad")
	alias := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "bad"
	for _, alternate := range []Overlay{{alias, []byte("other"), source.YAML}, {alias, []byte("same"), source.RawLF}} {
		overlays := []Overlay{{path, []byte("same"), source.YAML}, alternate, {filepath.Join(dir, "good"), []byte("good"), source.YAML}}
		var message string
		for range 2 {
			store, err := New(t.Context(), overlays, func(context.Context, string) ([]byte, error) { return []byte("disk"), nil })
			if store == nil || err != nil {
				t.Fatalf("constructor rejected conflicting document: %v", err)
			}
			cleanupStore(t, store)
			_, _, err = store.Lookup(path)
			conflict := requireConflict(t, err)
			if conflict.document != sourceid.Document(fsread.PhysicalPath(path)) {
				t.Fatalf("conflict document = %q", conflict.document)
			}
			if message != "" && err.Error() != message {
				t.Fatalf("order changed conflict: %q != %q", err.Error(), message)
			}
			message = err.Error()
			for _, address := range []string{path, alias} {
				_, err := store.Read(t.Context(), address, source.YAML)
				requireConflict(t, err)
			}
			if got := readSnapshot(t, store, filepath.Join(dir, "good"), source.YAML); string(got.Bytes()) != "good" {
				t.Fatal("unrelated overlay unavailable")
			}
			if got := readSnapshot(t, store, filepath.Join(dir, "disk"), source.RawLF); string(got.Bytes()) != "disk" {
				t.Fatal("unrelated read unavailable")
			}
			slices.Reverse(overlays)
		}
	}
}

func TestCoordinateMismatchPreservesAcceptedCapture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	store := newStore(t, []Overlay{{path, []byte("abc\r\n"), source.RawLF}}, func(context.Context, string) ([]byte, error) { return nil, nil })
	snapshot := readSnapshot(t, store, path, source.RawLF)
	_, err := store.Read(t.Context(), path, source.YAML)
	requireConflict(t, err)
	requireConflict(t, store.Verify(snapshot, []byte("abc\r\n")))
	if string(snapshot.Bytes()) != "abc\r\n" || snapshot.Coordinates() != source.RawLF {
		t.Fatal("quarantine rewrote private accepted bytes/model")
	}
}

func TestVerifyRejectsMismatchedBytesAndHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	reader := func(context.Context, string) ([]byte, error) { return []byte("captured"), nil }
	store := newStore(t, nil, reader)
	snapshot := readSnapshot(t, store, path, source.YAML)
	requireConflict(t, store.Verify(snapshot, []byte("changed")))
	if err := store.Verify(Snapshot{}, nil); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("zero handle = %v", err)
	}
	other := newStore(t, nil, reader)
	if err := other.Verify(snapshot, snapshot.Bytes()); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("foreign handle = %v", err)
	}
	stale := snapshot
	copyValue := *snapshot.value
	stale.value = &copyValue
	requireConflict(t, store.Verify(stale, snapshot.Bytes()))
	if err := store.Verify(snapshot, []byte("captured")); err != nil {
		t.Fatal(err)
	}
}

func TestStdinAndInvalidUTF8(t *testing.T) {
	invalid := []byte{0xff, '\r', '\n'}
	store := newStore(t, []Overlay{{"<stdin>", invalid, source.RawLF}}, func(context.Context, string) ([]byte, error) { return nil, nil })
	snapshot := readSnapshot(t, store, "<stdin>", source.RawLF)
	if snapshot.Revision().Document() != "<stdin>" || !snapshot.Revision().Valid() || snapshot.CoordinatesAvailable() || snapshot.Coordinates() != source.RawLF {
		t.Fatalf("invalid UTF-8 capture = %#v", snapshot)
	}
	if snapshot.Revision().Digest() != sourceid.Digest(invalid) || !bytes.Equal(snapshot.Bytes(), invalid) {
		t.Fatal("invalid UTF-8 bytes lost")
	}
	if document, err := store.Canonical(t.Context(), "<stdin>"); err != nil || document != "<stdin>" {
		t.Fatalf("stdin canonical = %q, %v", document, err)
	}
}

func TestCanonicalSymlinkAndMissingParentMemoized(t *testing.T) {
	dir := t.TempDir()
	physicalRoot := filepath.Join(dir, "real")
	if err := os.Mkdir(physicalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(physicalRoot, link); err != nil {
		// Windows reports ERROR_PRIVILEGE_NOT_HELD separately from ErrPermission.
		const privilegeNotHeld = syscall.Errno(1314)
		if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) ||
			(runtime.GOOS == "windows" && errors.Is(err, privilegeNotHeld)) {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	alias := filepath.Join(link, "missing-parent", "file")
	physical := filepath.Join(physicalRoot, "missing-parent", "file")
	store := newStore(t, []Overlay{{alias, []byte("captured"), source.YAML}}, func(context.Context, string) ([]byte, error) {
		t.Error("canonical alias missed overlay")
		return nil, errors.New("canonical alias missed overlay")
	})
	resolvedReal, err := filepath.EvalSymlinks(physicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := sourceid.Document(filepath.Join(resolvedReal, "missing-parent", "file"))
	if got := readSnapshot(t, store, physical, source.YAML); got.Revision().Document() != want {
		t.Fatalf("revision document = %q", got.Revision().Document())
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Canonical(t.Context(), alias); err != nil || got != want {
		t.Fatalf("memoized identity = %q, %v", got, err)
	}
}

func TestMalformedConfigurationReturnsNoStore(t *testing.T) {
	reader := func(context.Context, string) ([]byte, error) { return nil, nil }
	for _, overlays := range [][]Overlay{{{"", nil, source.YAML}}, {{filepath.Join(t.TempDir(), "file"), nil, ""}}, {{"a\x00b", nil, source.RawLF}}, {{"relative", nil, source.RawLF}}} {
		if store, err := New(t.Context(), overlays, reader); store != nil || err == nil {
			t.Fatalf("accepted malformed config: %v, %v", store != nil, err)
		}
	}
	if store, err := New(t.Context(), nil, nil); store != nil || err == nil {
		t.Fatal("accepted nil reader")
	}
}

func TestReadAliasesExcludeUnusedOverlaysAndIdentityLookups(t *testing.T) {
	root := t.TempDir()
	used, unused := filepath.Join(root, "used.yml"), filepath.Join(root, "unused.yml")
	alias := root + string(filepath.Separator) + "." + string(filepath.Separator) + "used.yml"
	store := newStore(t, []Overlay{{used, []byte("used"), source.YAML}, {unused, []byte("unused"), source.YAML}}, func(context.Context, string) ([]byte, error) {
		return nil, os.ErrNotExist
	})
	if _, err := store.ReadAliases(); !errors.Is(err, ErrNotSealed) {
		t.Fatalf("unsealed read aliases = %v", err)
	}
	if _, err := store.Canonical(t.Context(), unused); err != nil {
		t.Fatal(err)
	}
	readSnapshot(t, store, alias, source.YAML)
	if _, err := store.Read(t.Context(), filepath.Join(root, "missing.yml"), source.YAML); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	paths, err := store.ReadAliases()
	if err != nil || !slices.Equal(paths, []string{alias}) {
		t.Fatalf("consumed aliases = %v, error=%v", paths, err)
	}
	paths[0] = "changed"
	if again, err := store.ReadAliases(); err != nil || !slices.Equal(again, []string{alias}) {
		t.Fatalf("caller mutated read aliases = %v, error=%v", again, err)
	}
}

func TestReadFailureShowsCaptureCause(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yml")
	cause := &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	store := newStore(t, nil, func(context.Context, string) ([]byte, error) { return nil, cause })
	_, err := store.Read(t.Context(), path, source.YAML)
	if err == nil {
		t.Fatal("missing file was read")
	}
	failure := &ReadFailure{Context: "could not read workflow", Err: err}
	if want := "could not read workflow: " + cause.Error(); failure.Error() != want {
		t.Fatalf("message = %q, want %q", failure.Error(), want)
	}
	var captured *CaptureError
	if !errors.As(failure, &captured) || captured.Code() != "capture-failed" || !errors.Is(failure, os.ErrNotExist) {
		t.Fatalf("read failure lost the capture error or its cause: %v", failure)
	}
	other := errors.New("not a capture")
	if got := (&ReadFailure{Context: "ctx", Err: other}).Error(); got != "ctx: not a capture" {
		t.Fatalf("non-capture message = %q", got)
	}
}
