//go:build unix

package gitrepo

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

func deviceID(path string) (uint64, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return 0, &fs.PathError{Op: "stat", Path: path, Err: err}
	}
	return widen(stat.Dev), nil
}

func widen[T int32 | uint32 | int64 | uint64](value T) uint64 {
	return uint64(value)
}

func searchable(path string) bool {
	return unix.Access(path, unix.X_OK) == nil
}
