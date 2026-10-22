package component

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestComponentLoadRejectsOversizedFileBeforeParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.wasm")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(128*1024*1024 + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	plugin, err := Load(t.Context(), path, Options{})
	if plugin != nil {
		_ = plugin.Close(t.Context())
	}
	if plugin != nil || err == nil || !strings.Contains(err.Error(), "component exceeds size limit of 134217728 bytes") {
		t.Fatalf("oversized file reached component parsing: plugin=%v err=%v", plugin, err)
	}
}

func TestComponentFileReadAcceptsBoundAndRejectsOverflow(t *testing.T) {
	const limit = 4096
	path := filepath.Join(t.TempDir(), "component.wasm")
	for _, size := range []int{limit - 1, limit, limit + 1} {
		content := bytes.Repeat([]byte{0}, size)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		data, err := readComponentFile(t.Context(), path, limit)
		if size <= limit {
			if err != nil || !bytes.Equal(data, content) {
				t.Fatalf("size=%d bytes=%d err=%v", size, len(data), err)
			}
		} else if data != nil || err == nil || !strings.Contains(err.Error(), "size limit of 4096 bytes") {
			t.Fatalf("overflow accepted: bytes=%d err=%v", len(data), err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readComponentFile(ctx, path, limit); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition lost cancellation: %v", err)
	}
}

func TestComponentFileReadBoundsUnendingDevice(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("requires /dev/zero")
	}
	if data, err := readComponentFile(t.Context(), "/dev/zero", 4096); data != nil || err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("unending device escaped read bound: bytes=%d err=%v", len(data), err)
	}
}

func TestComponentFileReadCancellationInterruptsBlockedStream(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc/self/fd")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := readComponentFile(ctx, fmt.Sprintf("/proc/self/fd/%d", reader.Fd()), 4096)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked read lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked stream read ignored cancellation")
	}
}
