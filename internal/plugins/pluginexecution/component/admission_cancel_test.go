package component

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAdmissionCanceledWhileCompilingIsNoRejection(t *testing.T) {
	t.Parallel()
	valid, err := os.ReadFile(filepath.Join("testdata", "plugins", "no-sudo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sharedComponentCompilation.gate.Acquire(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := Admit(ctx, ComponentBytes(valid))
		done <- err
	}()
	cancel()
	err = <-done
	sharedComponentCompilation.gate.Release(1)
	if !errors.Is(err, context.Canceled) || errors.As(err, new(*AdmissionError)) {
		t.Fatalf("admission canceled while compiling: %v", err)
	}
}
