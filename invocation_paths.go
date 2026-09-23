package actionlint

import (
	"path"
	"strings"
	"unicode"
)

func (rule *RuleExecutableBit) checkedRunnerPath(runnerPath string) (string, bool) {
	snapshot := rule.index()
	if snapshot.err != nil {
		return "", false
	}
	if rule.caseInsensitive {
		var known bool
		runnerPath, known = snapshot.caseFoldedPath(runnerPath, rule.paths.checkout)
		if !known {
			return "", false
		}
	}
	return runnerPath, snapshot.ordinaryTraversal(runnerPath, rule.paths.checkout)
}

// Resolve components before collapsing '..', retaining ambiguous index names as
// unknown. Limit folding to ASCII rather than guessing APFS Unicode normalization.
func (snapshot *gitModeSnapshot) caseFoldedPath(runnerPath, checkout string) (string, bool) {
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
		if i < len(parts)-1 && !snapshot.ordinaryTraversal(candidate+"/", checkout) {
			return "", false
		}
		location = candidate
	}
	if strings.HasSuffix(runnerPath, "/") {
		location += "/"
	}
	return location, true
}

func (snapshot *gitModeSnapshot) foldedPrefix(candidate, checkout string) (string, bool) {
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
			return prefix, asciiPath(prefix) && asciiPath(candidate)
		}
		relative = strings.Join(parts[len(root):], "/")
	}
	canonical, exists := snapshot.folded[pathFoldKey(relative)]
	if !exists {
		return candidate, true
	}
	if canonical == "" || !asciiPath(candidate) || !asciiPath(checkout) {
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

func asciiPath(value string) bool {
	for _, c := range value {
		if c > 127 {
			return false
		}
	}
	return true
}
