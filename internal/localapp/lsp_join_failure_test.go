package localapp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/langserver"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/source"
)

func TestEditorFailedCaptureJoinReturnsWithoutPublishingSources(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	lifetime, stopLifetime := context.WithCancel(context.Background())
	store, err := capture.New(lifetime, nil, func(context.Context, string) ([]byte, error) {
		close(started)
		<-release
		return []byte("captured only after release"), nil
	})
	if err != nil {
		stopLifetime()
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "workflow.yml")
	readDone := make(chan error, 1)
	readerFinished := make(chan struct{})
	go func() { _, err := store.Read(lifetime, path, source.YAML); readDone <- err; close(readerFinished) }()
	<-started
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(func() {
		defer stopLifetime()
		unblock()
		if err := <-readDone; err != nil {
			t.Error(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	joinErr := store.Close(expired)
	if _, failed := errors.AsType[*capture.JoinError](joinErr); !failed || !errors.Is(joinErr, context.DeadlineExceeded) {
		t.Fatalf("fixture did not leave real pending capture work: %v", joinErr)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	partial, initErr := capture.New(cancelled, nil, func(context.Context, string) ([]byte, error) {
		t.Error("cancelled initialization read content")
		return nil, nil
	})
	initialization, typed := errors.AsType[*capture.InitError](initErr)
	if partial != nil || !typed || !errors.Is(initErr, context.Canceled) || initialization.Code() != "cancelled" || initialization.Stage() != "capture" {
		t.Fatalf("initialization cancellation classification lost: %v", initErr)
	}
	publication, logs := firstEditorPublication(t, func(context.Context, map[string]langserver.Document) (langserver.Analysis, error) {
		return langserver.Analysis{}, errors.Join(initErr, joinErr)
	}, langserver.FileURI(path), "open buffer metadata")
	if len(publication.Params.Diagnostics) != 0 || len(logs) != 1 || !strings.Contains(logs[0], "initialization") || !strings.Contains(logs[0], "join failed") {
		t.Fatalf("failed capture leaked coordinates or hid its join failure: publication=%+v logs=%v", publication, logs)
	}
	// firstEditorPublication has completed shutdown and joined Serve's workers.
	// The failed capture remains pending, owned by this test until cleanup.
	select {
	case <-readerFinished:
		t.Fatal("transport proof did not retain pending capture work")
	default:
	}
}
