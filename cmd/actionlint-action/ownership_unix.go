//go:build unix

package main

import (
	"os"
	"syscall"
)

func inheritOwner(root *os.Root, paths []string) error {
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	for _, p := range paths {
		if err := root.Chown(p, int(owner.Uid), int(owner.Gid)); err != nil {
			return err
		}
	}
	return nil
}
