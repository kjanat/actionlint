//go:build unix

package gitrepo

import "golang.org/x/sys/unix"

func readable(path string) bool {
	return unix.Access(path, unix.R_OK) == nil
}
