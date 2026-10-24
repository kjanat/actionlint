//go:build unix

package pluginregistry

import (
	"os"

	"golang.org/x/sys/unix"
)

func openCandidateFile(root *os.Root, name string) (*os.File, error) {
	// Reject FIFOs through f.Stat without waiting for a writer in open.
	return root.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
