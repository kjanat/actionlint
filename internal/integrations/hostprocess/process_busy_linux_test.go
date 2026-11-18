package hostprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRunRetriesBusyExecutable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "provider")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nread -r input\nprintf '%s' \"$input\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := exec.CommandContext(t.Context(), path).Run(); !errors.Is(err, syscall.ETXTBSY) {
		t.Fatalf("starting a busy executable returned %v, want ETXTBSY", err)
	}
	execution := Execution{Cmd: path, Stdin: "one input\n", RequireSuccess: true, OutputLimit: 1024}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := execution.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelling a busy start returned %v, want DeadlineExceeded", err)
	}
	timer := time.AfterFunc(50*time.Millisecond, func() { _ = writer.Close() })
	defer timer.Stop()
	if output, err := execution.Run(t.Context()); err != nil || string(output) != "one input" {
		t.Fatalf("output = %q, error = %v; want one input", output, err)
	}
}
