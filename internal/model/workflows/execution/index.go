package execution

import (
	"path"
	"strings"
	"sync"
	"unicode"
)

type IndexSnapshot struct {
	dirsOnce sync.Once
	foldOnce sync.Once
	modes    map[string]string
	dirs     map[string]struct{}
	folded   map[string]string
	path     string
	err      error
}

// NewIndexSnapshot takes ownership of modes, which must not change afterwards.
func NewIndexSnapshot(path string, modes map[string]string, err error) *IndexSnapshot {
	return &IndexSnapshot{modes: modes, path: path, err: err}
}

// Mode returns "" when the path is not tracked.
func (snapshot *IndexSnapshot) Mode(name string) string { return snapshot.modes[name] }

func (snapshot *IndexSnapshot) Path() string { return snapshot.path }

func (snapshot *IndexSnapshot) Err() error { return snapshot.err }

func (snapshot *IndexSnapshot) prepareDirectories() {
	snapshot.dirsOnce.Do(func() {
		snapshot.dirs = make(map[string]struct{})
		for name := range snapshot.modes {
			for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
				if _, exists := snapshot.dirs[directory]; exists {
					break
				}
				snapshot.dirs[directory] = struct{}{}
			}
		}
	})
}

func (snapshot *IndexSnapshot) prepareFoldedPaths() {
	snapshot.foldOnce.Do(func() {
		snapshot.prepareDirectories()
		snapshot.folded = make(map[string]string)
		add := func(prefix string) {
			key := pathFoldKey(prefix)
			canonical, exists := snapshot.folded[key]
			if !ASCIIPath(prefix) || exists && canonical != prefix {
				snapshot.folded[key] = ""
			} else {
				snapshot.folded[key] = prefix
			}
		}
		for name := range snapshot.modes {
			add(name)
		}
		for directory := range snapshot.dirs {
			add(directory)
		}
	})
}

// CaseFoldedPath resolves path components against the index before collapsing '..' and returns false for ambiguous case matches.
func (snapshot *IndexSnapshot) CaseFoldedPath(runnerPath, checkout string) (string, bool) {
	snapshot.prepareFoldedPaths()
	parts := strings.Split(runnerPath, "/")
	location := ""
	for i, part := range parts {
		candidate := path.Join(location, part)
		if candidate == ".." || strings.HasPrefix(candidate, "../") {
			return "", false
		}
		var known bool
		candidate, known = snapshot.foldedPrefix(candidate, checkout)
		if !known {
			return "", false
		}
		if i < len(parts)-1 && !snapshot.OrdinaryTraversal(candidate+"/", checkout) {
			return "", false
		}
		location = candidate
	}
	if strings.HasSuffix(runnerPath, "/") {
		location += "/"
	}
	return location, true
}

func (snapshot *IndexSnapshot) foldedPrefix(candidate, checkout string) (string, bool) {
	relative := candidate
	checkout = path.Clean(checkout)
	if checkout != "." {
		parts, root := strings.Split(candidate, "/"), strings.Split(checkout, "/")
		depth := min(len(parts), len(root))
		prefix := strings.Join(root[:depth], "/")
		if !strings.EqualFold(strings.Join(parts[:depth], "/"), prefix) {
			return candidate, true
		}
		if len(parts) <= len(root) {
			if len(snapshot.modes) == 0 {
				return candidate, true
			}
			return prefix, ASCIIPath(prefix) && ASCIIPath(candidate)
		}
		relative = strings.Join(parts[len(root):], "/")
	}
	canonical, exists := snapshot.folded[pathFoldKey(relative)]
	if !exists {
		return candidate, true
	}
	if canonical == "" || !ASCIIPath(candidate) || !ASCIIPath(checkout) {
		return "", false
	}
	return path.Join(checkout, canonical), true
}

// pathFoldKey matches strings.EqualFold, so non-ASCII names that fold to ASCII still make a matching prefix unknown.
func pathFoldKey(value string) string {
	return strings.Map(func(r rune) rune {
		canonical := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			canonical = min(canonical, next)
		}
		return canonical
	}, value)
}

func ASCIIPath(value string) bool {
	for _, c := range value {
		if c > 127 {
			return false
		}
	}
	return true
}

func (snapshot *IndexSnapshot) OrdinaryTraversal(runnerPath, checkout string) bool {
	parts := strings.Split(runnerPath, "/")
	location := ""
	checkout = path.Clean(checkout)
	if checkout == "." {
		checkout = ""
	}
	for i, part := range parts {
		location = path.Join(location, part)
		if location == ".." || strings.HasPrefix(location, "../") {
			return false
		}
		name := location
		if checkout != "" {
			var within bool
			name, within = strings.CutPrefix(location, checkout+"/")
			if !within {
				if location == "." || location == checkout || strings.HasPrefix(checkout, location+"/") {
					continue
				}
				return false
			}
		}
		if i < len(parts)-1 && !snapshot.DirectoryExists(name) {
			return false
		}
	}
	return true
}

func (snapshot *IndexSnapshot) DirectoryExists(name string) bool {
	if name == "" || name == "." {
		return true
	}
	if snapshot.modes[name] != "" {
		return false
	}
	snapshot.prepareDirectories()
	_, exists := snapshot.dirs[name]
	return exists
}
