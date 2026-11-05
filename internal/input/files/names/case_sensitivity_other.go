//go:build !darwin

package names

import "os"

func directoryCaseInsensitive(path string, stat func(string) (os.FileInfo, error), readDir func(string) ([]os.DirEntry, error)) bool {
	return caseInsensitiveDirectoryEntries(path, stat, readDir)
}
