//go:build !unix

package gitrepo

import "os"

// Git for Windows reports st_dev as 0 and drops X_OK from access(2).
func deviceID(string) (uint64, error) {
	return 0, nil
}

func searchable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
