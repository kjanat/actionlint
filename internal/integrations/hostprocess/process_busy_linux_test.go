package hostprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
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
	const stdin = "one input\n"
	execution := Execution{Cmd: path, Stdin: stdin, RequireSuccess: true, OutputLimit: 1024}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := execution.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelling a busy start returned %v, want DeadlineExceeded", err)
	}
	var waits atomic.Int32
	execution.busyRetryWait = func(ctx context.Context, delay time.Duration) error {
		if waits.Add(1) == 1 {
			if err := writer.Close(); err != nil {
				return err
			}
		}
		return waitForBusyRetry(ctx, delay)
	}
	output, err := execution.Run(t.Context())
	if err != nil {
		t.Fatalf("retrying a busy start returned %v", err)
	}
	if waits.Load() < 1 {
		t.Fatal("retry started without waiting after ETXTBSY")
	}
	if string(output) != "one input" {
		t.Fatalf("retry received stdin %q, want %q", output, "one input")
	}
}
