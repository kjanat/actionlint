//go:build !unix

package fsread

import "os"

func openSnapshotFile(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
