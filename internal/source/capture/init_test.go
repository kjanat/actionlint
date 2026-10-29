package capture

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/source"
)

func TestNewCancelledMetadataWaitRetainsJoinOwnership(t *testing.T) {
	root := t.TempDir()
	first, later := filepath.Join(root, "first.yml"), filepath.Join(root, "later.yml")
	started, release := make(chan struct{}), make(chan struct{})
	signalStarted := sync.OnceFunc(func() { close(started) })
	unblock := sync.OnceFunc(func() { close(release) })
	var resolutions, resolving, reads atomic.Int32
	resolvedPaths := make(chan string, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		store *Store
		err   error
	}
	returned := make(chan outcome, 1)
	constructionDone := make(chan struct{})
	var result outcome
	gotResult := false
	t.Cleanup(func() {
		cancel()
		unblock()
		select {
		case <-constructionDone:
		case <-time.After(time.Second):
			t.Error("constructor did not exit after releasing metadata")
			return
		}
		if !gotResult {
			result = <-returned
		}
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if result.store != nil {
			if err := result.store.Close(cleanup); err != nil {
				t.Errorf("constructor regression cleanup: %v", err)
			}
		} else if initialization, ok := errors.AsType[*InitError](result.err); ok {
			if err := initialization.Join(cleanup); err != nil {
				t.Errorf("failed constructor cleanup: %v", err)
			}
		}
	})
	go func() {
		defer close(constructionDone)
		store, err := newStoreWithResolver(ctx, []Overlay{
			{Path: first, Content: []byte("first"), Coordinates: source.YAML},
			{Path: later, Content: []byte("later"), Coordinates: source.YAML},
		}, func(context.Context, string) ([]byte, error) {
			reads.Add(1)
			return nil, nil
		}, func(path string) string {
			resolutions.Add(1)
			resolvedPaths <- path
			resolving.Add(1)
			defer resolving.Add(-1)
			signalStarted()
			<-release
			return path
		})
		returned <- outcome{store, err}
	}()
	select {
	case <-started:
		if path := <-resolvedPaths; path != first {
			t.Fatalf("first resolved overlay=%q, want %q", path, first)
		}
	case <-time.After(time.Second):
		t.Fatal("constructor did not start metadata resolution")
	}
	cancel()
	select {
	case result = <-returned:
		gotResult = true
	case <-time.After(time.Second):
		t.Fatal("constructor cancellation waited for blocked OS metadata")
	}
	initialization, ok := errors.AsType[*InitError](result.err)
	if result.store != nil || !ok || !errors.Is(result.err, context.Canceled) || !initialization.Pending() {
		t.Fatalf("cancelled constructor = %v, %v; want nil Store and pending InitError", result.store, result.err)
	}
	joinCtx, stop := context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer stop()
	joinErr := initialization.Join(joinCtx)
	if _, ok := errors.AsType[*JoinError](joinErr); !ok || !errors.Is(joinErr, context.DeadlineExceeded) || !initialization.Pending() {
		t.Fatalf("blocked join lost pending ownership: %v", joinErr)
	}
	joinedFailure := errors.Join(result.err, joinErr)
	classified, ok := errors.AsType[*InitError](joinedFailure)
	if !ok || classified.Code() != "cancelled" || classified.Stage() != "capture" || !errors.Is(joinedFailure, context.Canceled) || !errors.Is(joinedFailure, context.DeadlineExceeded) {
		t.Fatalf("joined cleanup lost typed initialization classification: %v", joinedFailure)
	}
	if resolutions.Load() != 1 || reads.Load() != 0 || resolving.Load() != 1 {
		t.Fatalf("cancelled constructor work: resolutions=%d reads=%d resolving=%d", resolutions.Load(), reads.Load(), resolving.Load())
	}
	unblock()
	select {
	case <-initialization.pending:
	case <-time.After(time.Second):
		t.Fatal("released metadata did not finish")
	}
	if !initialization.Pending() || resolving.Load() != 0 {
		t.Fatalf("completed metadata lost unjoined ownership: pending=%t resolving=%d", initialization.Pending(), resolving.Load())
	}
	joined, done := context.WithTimeout(t.Context(), time.Second)
	defer done()
	if err := initialization.Join(joined); err != nil || initialization.Pending() || resolving.Load() != 0 {
		t.Fatalf("released work not joined: pending=%t resolving=%d error=%v", initialization.Pending(), resolving.Load(), err)
	}
	if err := initialization.Join(joinCtx); err != nil {
		t.Fatalf("already joined initialization waited again: %v", err)
	}
	if resolutions.Load() != 1 || reads.Load() != 0 {
		t.Fatalf("later work started after failure: resolutions=%d reads=%d", resolutions.Load(), reads.Load())
	}
}

func TestNewValidatesEverythingBeforeStartingMetadata(t *testing.T) {
	valid := Overlay{Path: filepath.Join(t.TempDir(), "valid.yml"), Coordinates: source.YAML}
	for _, tc := range []struct {
		name     string
		overlays []Overlay
		cancel   bool
		deadline bool
	}{
		{name: "pre-cancelled", overlays: []Overlay{valid}, cancel: true},
		{name: "pre-deadline", overlays: []Overlay{valid}, deadline: true},
		{name: "later relative address", overlays: []Overlay{valid, {Path: "relative", Coordinates: source.YAML}}},
		{name: "later missing coordinates", overlays: []Overlay{valid, {Path: valid.Path}}},
		{name: "later NUL", overlays: []Overlay{valid, {Path: "invalid\x00", Coordinates: source.YAML}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			if tc.deadline {
				deadlineCtx, stop := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer stop()
				ctx = deadlineCtx
			}
			var resolutions, reads atomic.Int32
			store, err := newStoreWithResolver(ctx, tc.overlays, func(context.Context, string) ([]byte, error) {
				reads.Add(1)
				return nil, nil
			}, func(path string) string { resolutions.Add(1); return path })
			initialization, ok := errors.AsType[*InitError](err)
			if store != nil || !ok || initialization.Pending() || resolutions.Load() != 0 || reads.Load() != 0 {
				t.Fatalf("preflight started work: store=%v error=%v resolutions=%d reads=%d", store, err, resolutions.Load(), reads.Load())
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation cause: %v", err)
			}
			wantCode := "capture-failed"
			if tc.cancel || tc.deadline {
				wantCode = "cancelled"
			}
			if initialization.Code() != wantCode || initialization.Stage() != "capture" {
				t.Fatalf("initialization classification: code=%q stage=%q", initialization.Code(), initialization.Stage())
			}
			if tc.deadline && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lost deadline cause: %v", err)
			}
			if joinErr := initialization.Join(ctx); joinErr != nil {
				t.Fatalf("idle initialization required join: %v", joinErr)
			}
		})
	}
}

func TestNewOverlayAliasesShareCanonicalMemoAndMergeDocuments(t *testing.T) {
	root := t.TempDir()
	a, b, document := filepath.Join(root, "a.yml"), filepath.Join(root, "b.yml"), filepath.Join(root, "document.yml")
	for _, tc := range []struct {
		name    string
		content string
		model   source.Coordinates
		state   State
	}{
		{"matching", "same", source.YAML, Captured},
		{"content conflict", "other", source.YAML, Conflicted},
		{"model conflict", "same", source.RawLF, Conflicted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var conflictMessage string
			for _, reverse := range []bool{false, true} {
				overlays := []Overlay{
					{Path: a, Content: []byte("same"), Coordinates: source.YAML},
					{Path: b, Content: []byte(tc.content), Coordinates: tc.model},
					{Path: a, Content: []byte("same"), Coordinates: source.YAML},
				}
				if reverse {
					slices.Reverse(overlays)
				}
				counts := make(map[string]int)
				var reads atomic.Int32
				store, err := newStoreWithResolver(t.Context(), overlays, func(context.Context, string) ([]byte, error) {
					reads.Add(1)
					return nil, nil
				}, func(path string) string { counts[path]++; return document })
				if err != nil {
					t.Fatal(err)
				}
				cleanupStore(t, store)
				store.mu.Lock()
				active := store.active
				store.mu.Unlock()
				if active != 0 {
					t.Fatalf("successful constructor retained %d tasks", active)
				}
				for _, path := range []string{a, b} {
					if got, err := store.Canonical(t.Context(), path); err != nil || got != document {
						t.Fatalf("overlay memo = %q, %v", got, err)
					}
				}
				if counts[a] != 1 || counts[b] != 1 || len(counts) != 2 || reads.Load() != 0 {
					t.Fatalf("constructor did not share metadata memo: counts=%v reads=%d", counts, reads.Load())
				}
				if err := store.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
				rows, err := store.Sources()
				if err != nil || len(rows) != 1 || rows[0].Document != document || rows[0].State != tc.state || !slices.Equal(rows[0].Aliases, []string{a, b}) {
					t.Fatalf("registered overlay documents not merged: rows=%+v error=%v", rows, err)
				}
				if tc.state == Conflicted {
					if rows[0].Coordinates != "" || rows[0].CoordinatesAvailable || rows[0].Revision != (source.Revision{}) {
						t.Fatalf("conflicted row exposed first-arrival coordinates/revision: %+v", rows[0])
					}
					for _, path := range []string{a, b} {
						captured, state, err := store.Lookup(path)
						if captured.Valid() || state != Conflicted {
							t.Fatalf("conflicted alias exposed captured evidence: state=%s snapshot=%t", state, captured.Valid())
						}
						requireConflict(t, err)
					}
					if !reverse {
						conflictMessage = rows[0].Message
					} else if rows[0].Message != conflictMessage {
						t.Fatalf("overlay order changed conflict: %q != %q", rows[0].Message, conflictMessage)
					}
				} else if captured, state, err := store.Lookup(a); err != nil || state != Captured || string(captured.Bytes()) != "same" {
					t.Fatalf("matching overlay unavailable: state=%s error=%v", state, err)
				}
				if aliases, err := store.ReadAliases(); err != nil || len(aliases) != 0 {
					t.Fatalf("overlay resolution read content: aliases=%v error=%v", aliases, err)
				}
			}
		})
	}
}
