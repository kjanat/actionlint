//go:build !unix

package gitrepo

import "os"

func readable(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
