//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris || zos

package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

func terminalWidth(file *os.File) int {
	size, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(size.Col)
}
