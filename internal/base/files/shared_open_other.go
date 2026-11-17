//go:build !windows

package files

import "os"

func openSharedFile(path string) (*os.File, error) { return os.Open(path) }
