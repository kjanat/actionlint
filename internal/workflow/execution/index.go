package execution

import (
	"path"
	"strings"
	"sync"
	"unicode"
)

// IndexSnapshot indexes paths and executable modes supplied by the caller.
type IndexSnapshot struct {
	dirsOnce sync.Once
	foldOnce sync.Once
	Modes    map[string]string
	dirs     map[string]struct{}
	folded   map[string]string
	Path     string
	Err      error
}

// Modes are immutable after loading. Share the derived directory and folding
// indexes across all invocations, including those using different checkout paths.
func (snapshot *IndexSnapshot) prepareDirectories() {
	snapshot.dirsOnce.Do(func() {
		snapshot.dirs = make(map[string]struct{})
		for name := range snapshot.Modes {
			for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
				if _, exists := snapshot.dirs[directory]; exists {
					break // Its ancestors were added with the first descendant.
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
				snapshot.folded[key] = "" // Ambiguous or unsupported normalization.
			} else {
				snapshot.folded[key] = prefix
			}
		}
		for name := range snapshot.Modes {
			add(name)
		}
		for directory := range snapshot.dirs {
			add(directory)
		}
	})
}

// CaseFoldedPath resolves ASCII path components against the index before
// collapsing '..'. Ambiguous case matches return false.
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
			if len(snapshot.Modes) == 0 {
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

// Match strings.EqualFold's equivalence classes, including non-ASCII names that
// fold to ASCII. Such names must still make a matching prefix unknown.
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
	if snapshot.Modes[name] != "" {
		return false
	}
	snapshot.prepareDirectories()
	_, exists := snapshot.dirs[name]
	return exists
}
