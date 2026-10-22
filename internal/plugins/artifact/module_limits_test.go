package artifact

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRawModuleReadStopsAtBound(t *testing.T) {
	const limit = 4096
	path := filepath.Join(t.TempDir(), "large.wasm")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(limit + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if artifact, err := Resolve(t.Context(), path, Options{MaxBytes: limit}); artifact != nil || err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized raw file reached parsing: artifact=%v err=%v", artifact, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readFile(ctx, path, limit); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled module read: %v", err)
	}
}

func TestRawModuleReadBoundsUnendingDevice(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("requires /dev/zero")
	}
	if data, err := readFile(t.Context(), "/dev/zero", 4096); data != nil || err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("unending device escaped read bound: bytes=%d err=%v", len(data), err)
	}
}

func TestRawModuleReadCancellationInterruptsBlockedStream(t *testing.T) {
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
		_, err := readFile(ctx, fmt.Sprintf("/proc/self/fd/%d", reader.Fd()), 4096)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked stream read lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked stream read ignored cancellation")
	}
}

func TestModuleSizeLimitIncludesDecompressedBytes(t *testing.T) {
	const limit = 1024
	for _, size := range []int{limit - 1, limit, limit + 1} {
		for _, packed := range []bool{false, true} {
			data := bytes.Repeat([]byte{0}, size)
			var reader io.Reader = bytes.NewReader(data)
			if packed {
				gzipReader, err := gzip.NewReader(bytes.NewReader(compressed(t, data)))
				if err != nil {
					t.Fatal(err)
				}
				defer gzipReader.Close()
				reader = gzipReader
			}
			module, err := readBounded(reader, limit)
			if size <= limit {
				if err != nil || len(module) != size {
					t.Fatalf("size=%d compressed=%v: bytes=%d err=%v", size, packed, len(module), err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "limit") || module != nil {
				t.Fatalf("oversized module accepted: compressed=%v bytes=%d err=%v", packed, len(module), err)
			}
		}
	}
}

func TestModuleExpansionBombStopsAtBound(t *testing.T) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed(t, bytes.Repeat([]byte{0}, 1024*1024))))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := readBounded(reader, 4096); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expansion bomb was not rejected: %v", err)
	}
}
