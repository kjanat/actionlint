//go:build unix

package fsread

import (
	"os"

	"golang.org/x/sys/unix"
)

func openSnapshotFile(root *os.Root, name string) (*os.File, error) {
	// A file replaced with a FIFO must not block the host in open.
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
