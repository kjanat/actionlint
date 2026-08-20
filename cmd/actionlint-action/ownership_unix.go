//go:build unix

package main

import (
	"os"
	"syscall"
)

func inheritOwner(workspace string, paths []string) error {
	info, err := os.Stat(workspace)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	for _, p := range paths {
		if err := os.Chown(p, int(owner.Uid), int(owner.Gid)); err != nil {
			return err
		}
	}
	return nil
}
