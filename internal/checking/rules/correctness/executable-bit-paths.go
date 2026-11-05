package correctness

import (
	"path"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func (rule *ExecutableChecker) checkedRunnerPathFor(runnerPath string, paths execution.Paths) (string, bool) {
	snapshot := rule.index()
	if snapshot.Err() != nil {
		return "", false
	}
	checkout, known := paths.CheckoutFor(runnerPath)
	if paths.Placements.Present() {
		// Select before collapsing '..' to detect traversal through replaced copies.
		checkout, known = rule.traversalCheckout(paths.Placements, runnerPath)
	}
	if !known {
		return "", false
	}
	if rule.caseInsensitive {
		var known bool
		runnerPath, known = snapshot.CaseFoldedPath(runnerPath, checkout)
		if !known {
			return "", false
		}
	}
	return runnerPath, snapshot.OrdinaryTraversal(runnerPath, checkout)
}

func (rule *ExecutableChecker) traversalCheckout(placement execution.CheckoutPlacement, runnerPath string) (string, bool) {
	for strings.HasPrefix(runnerPath, "./") {
		runnerPath = strings.TrimPrefix(runnerPath, "./")
	}
	for current := placement; current.Present(); current = current.Previous() {
		prefix := path.Clean(current.Details().Directory.Path)
		candidate := runnerPath
		if rule.caseInsensitive {
			prefix, candidate = strings.ToLower(prefix), strings.ToLower(candidate)
		}
		if prefix == "." || candidate == prefix || strings.HasPrefix(candidate, prefix+"/") {
			return current.Details().Directory.Path, current.Details().Directory.Kind == execution.DirectoryKnown
		}
	}
	return "", false
}
