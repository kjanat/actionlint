package capture

import (
	"context"
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
)

func TestRemoteCaptureRejectsChangedContent(t *testing.T) {
	store := newStore(t, nil, func(context.Context, string) ([]byte, error) {
		t.Fatal("remote capture called the filesystem reader")
		return nil, nil
	})
	address := "https://github.com/owner/action/blob/abc/action.yml"
	content := []byte("name: original\n")
	first, err := store.CaptureRemote(t.Context(), address, content, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	content[0] = 'X'
	second, err := store.CaptureRemote(t.Context(), address, []byte("name: original\n"), source.YAML)
	if err != nil || first.Revision() != second.Revision() || string(second.Bytes()) != "name: original\n" {
		t.Fatalf("repeated capture changed source: %q, %v", second.Bytes(), err)
	}
	if first.Revision().Document() != address {
		t.Fatalf("remote source became a filesystem path: %s", first.Revision().Document())
	}
	_, err = store.CaptureRemote(t.Context(), address, []byte("name: changed\n"), source.YAML)
	requireConflict(t, err)
	requireConflict(t, store.VerifyHandle(first))
}

func TestRemoteCaptureHonorsCancellationAndClose(t *testing.T) {
	store := newStore(t, nil, func(context.Context, string) ([]byte, error) { return nil, nil })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	address := "https://github.com/owner/action/blob/abc/action.yml"
	if _, err := store.CaptureRemote(ctx, address, nil, source.YAML); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture: %v", err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CaptureRemote(t.Context(), address, nil, source.YAML); err == nil {
		t.Fatal("capture succeeded after close")
	}
}
