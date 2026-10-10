package workflownames

import (
	"os"
	"syscall"
)

func directoryCaseInsensitive(path string, _ func(string) (os.FileInfo, error), _ func(string) ([]os.DirEntry, error)) bool {
	return caseInsensitivePathconf(path, syscall.Pathconf)
}
