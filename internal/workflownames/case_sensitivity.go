package workflownames

import (
	"os"
	"path/filepath"
)

func caseInsensitivePathconf(path string, query func(string, int) (int, error)) bool {
	// Darwin's _PC_CASE_SENSITIVE is defined in xnu/bsd/sys/unistd.h.
	const caseSensitive = 11
	value, err := query(path, caseSensitive)
	return err == nil && value == 0
}

// Use existing entries as read-only evidence on filesystems without pathconf.
func caseInsensitiveDirectoryEntries(path string, stat func(string) (os.FileInfo, error), readDir func(string) ([]os.DirEntry, error)) bool {
	entries, err := readDir(path)
	if err != nil {
		return false
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, entry := range entries {
		name := entry.Name()
		alternate := []byte(name)
		for i, ch := range alternate {
			if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
				alternate[i] ^= 'a' - 'A'
				break
			}
		}
		if names[string(alternate)] || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		original, originalErr := stat(filepath.Join(path, name))
		folded, foldedErr := stat(filepath.Join(path, string(alternate)))
		return originalErr == nil && foldedErr == nil && os.SameFile(original, folded)
	}
	return false
}
