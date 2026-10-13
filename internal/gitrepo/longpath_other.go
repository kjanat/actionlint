//go:build !windows

package gitrepo

func longPath(path string) string {
	return path
}
