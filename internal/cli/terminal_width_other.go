//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !zos

package cli

import "os"

func terminalWidth(*os.File) int { return 0 }
