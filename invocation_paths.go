package actionlint

import (
	"path"
	"strings"
)

func (rule *RuleExecutableBit) checkedRunnerPathFor(runnerPath string, paths runPaths) (string, bool) {
	snapshot := rule.index()
	if snapshot.err != nil {
		return "", false
	}
	checkout, known := paths.checkoutFor(runnerPath)
	if paths.placements != nil {
		// Select before collapsing '..': traversal through a replaced copy must
		// remain unknown even if the final path leaves that copy again.
		checkout, known = rule.traversalCheckout(paths.placements, runnerPath)
	}
	if !known {
		return "", false
	}
	if rule.caseInsensitive {
		var known bool
		runnerPath, known = snapshot.caseFoldedPath(runnerPath, checkout)
		if !known {
			return "", false
		}
	}
	return runnerPath, snapshot.ordinaryTraversal(runnerPath, checkout)
}

func (rule *RuleExecutableBit) traversalCheckout(placement *checkoutPlacement, runnerPath string) (string, bool) {
	for strings.HasPrefix(runnerPath, "./") {
		runnerPath = strings.TrimPrefix(runnerPath, "./")
	}
	for current := placement; current != nil; current = current.previous {
		prefix := path.Clean(current.directory.path)
		candidate := runnerPath
		if rule.caseInsensitive {
			prefix, candidate = strings.ToLower(prefix), strings.ToLower(candidate)
		}
		if prefix == "." || candidate == prefix || strings.HasPrefix(candidate, prefix+"/") {
			return current.directory.path, current.directory.kind == directoryKnown
		}
	}
	return "", false
}

// Resolve components before collapsing '..', retaining ambiguous index names as
// unknown. Limit folding to ASCII rather than guessing APFS Unicode normalization.
func (snapshot *gitModeSnapshot) caseFoldedPath(runnerPath, checkout string) (string, bool) {
	parts := strings.Split(runnerPath, "/")
	location := ""
	for i, part := range parts {
		candidate := path.Join(location, part)
		if candidate == ".." || strings.HasPrefix(candidate, "../") {
			return "", false
		}
		canonical := ""
		depth := strings.Count(candidate, "/") + 1
		for name := range snapshot.modes {
			components := strings.Split(path.Join(checkout, name), "/")
			if len(components) < depth {
				continue
			}
			prefix := strings.Join(components[:depth], "/")
			if !strings.EqualFold(prefix, candidate) {
				continue
			}
			if !asciiPath(prefix) || !asciiPath(candidate) || canonical != "" && canonical != prefix {
				return "", false
			}
			canonical = prefix
		}
		if canonical != "" {
			candidate = canonical
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

func asciiPath(value string) bool {
	for _, c := range value {
		if c > 127 {
			return false
		}
	}
	return true
}
