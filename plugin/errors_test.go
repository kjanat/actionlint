package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

func TestFacadeRuntimeFailureSnapshot(t *testing.T) {
	path := &fs.PathError{Op: "read", Path: "snippet.py", Err: fs.ErrPermission}
	unsupported := &analyzer.UnsupportedToolConfigurationError{Reason: "unsupported option"}
	original := fmt.Errorf("plugin failed: %w", errors.Join(context.Canceled, io.ErrUnexpectedEOF, path, unsupported))
	want := original.Error()
	snapshot := snapshotRuntimeFailure(original)
	path.Op, path.Path, path.Err = "changed", "changed", io.EOF
	unsupported.Reason = "changed"
	if snapshot.Error() != want || !errors.Is(snapshot, context.Canceled) || !errors.Is(snapshot, io.ErrUnexpectedEOF) || !errors.Is(snapshot, fs.ErrPermission) || errors.Is(snapshot, io.EOF) {
		t.Fatalf("failure observations changed: %v", snapshot)
	}
	for range 2 {
		copiedPath, ok := errors.AsType[*fs.PathError](snapshot)
		if !ok || copiedPath.Op != "read" || copiedPath.Path != "snippet.py" || !errors.Is(copiedPath.Err, fs.ErrPermission) {
			t.Fatalf("path details lost: %v", copiedPath)
		}
		copiedPath.Op, copiedPath.Path, copiedPath.Err = "changed", "changed", io.EOF
		fallback, ok := errors.AsType[*analyzer.UnsupportedToolConfigurationError](snapshot)
		if !ok || fallback.Reason != "unsupported option" {
			t.Fatalf("fallback classification lost: %v", fallback)
		}
		fallback.Reason = "changed"
	}
}

func TestFacadeRuntimeFailureClasses(t *testing.T) {
	for _, class := range []error{context.Canceled, context.DeadlineExceeded, errors.ErrUnsupported, fs.ErrInvalid, fs.ErrPermission, fs.ErrExist, fs.ErrNotExist, fs.ErrClosed, fs.SkipDir, fs.SkipAll, io.EOF, io.ErrUnexpectedEOF, io.ErrClosedPipe, io.ErrNoProgress, io.ErrShortBuffer, io.ErrShortWrite} {
		t.Run(class.Error(), func(t *testing.T) {
			failure := snapshotRuntimeFailure(fmt.Errorf("runtime: %w", class))
			if !errors.Is(failure, class) {
				t.Fatalf("classification lost: %v", failure)
			}
		})
	}
	if snapshotRuntimeFailure(nil) != nil {
		t.Fatal("successful operation became a failure")
	}
}
