//go:build unix

package fsread

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSnapshotDoesNotBlockOnFIFO(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "source.sh")
	if err := unix.Mkfifo(file, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewSnapshot(t.Context(), directory, New(t.Context(), nil, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	finished := make(chan error, 1)
	go func() {
		f, err := snapshot.Open("source.sh")
		if f != nil {
			_ = f.Close()
		}
		finished <- err
	}()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("FIFO accepted as a regular source")
		}
	case <-time.After(2 * time.Second):
		fd, err := unix.Open(file, unix.O_RDWR|unix.O_NONBLOCK, 0)
		if err == nil {
			_ = unix.Close(fd)
		}
		t.Fatalf("source read blocked opening a FIFO: %v", <-finished)
	}
}
