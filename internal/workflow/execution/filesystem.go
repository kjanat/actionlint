package execution

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/fsread"
)

// AmbiguousPathError identifies multiple filesystem entries matching one runner path.
type AmbiguousPathError string

func (e AmbiguousPathError) Error() string {
	return fmt.Sprintf("ambiguous case-insensitive path %q", string(e))
}

// Use on-disk spelling for cache keys and diagnostics, even on a Linux host.
// Multiple case-equivalent entries cannot identify a unique runner action.
func caseInsensitiveRepositoryPath(root, spec string, reader *fsread.Reader) (string, bool) {
	local, err := CaseInsensitiveRepositoryPath(root, spec, reader)
	return local, err == nil
}

func CaseInsensitiveRepositoryPath(root, spec string, reader *fsread.Reader) (string, error) {
	dir := root
	for component := range strings.SplitSeq(strings.TrimPrefix(spec, "./"), "/") {
		if component == "" || component == "." || component == ".." {
			dir = filepath.Join(dir, component)
			continue
		}
		entries, err := reader.ReadDir(dir)
		if err != nil {
			return "", err
		}
		match := ""
		for _, entry := range entries {
			name := entry.Name()
			if name != component && (!ASCIIPath(name) || !ASCIIPath(component) || !strings.EqualFold(name, component)) {
				continue
			}
			if match != "" {
				return "", AmbiguousPathError(spec)
			}
			match = name
		}
		if match == "" {
			return "", os.ErrNotExist
		}
		dir = filepath.Join(dir, match)
	}
	local, err := filepath.Rel(root, dir)
	return "./" + filepath.ToSlash(local), err
}
