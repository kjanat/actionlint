//go:build unix

package githubaction

import (
	"os"
	"syscall"
)

// inheritOwner keeps reports writable by the workspace owner when the Docker
// compatibility entrypoint runs as root. Native non-root runs retain their owner.
func inheritOwner(root *os.Root, paths []string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	for _, path := range paths {
		if err := root.Chown(path, int(owner.Uid), int(owner.Gid)); err != nil {
			return err
		}
	}
	return nil
}
