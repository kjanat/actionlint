// Package references interprets action and reusable-workflow references.
package references

import (
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
)

// SplitAction splits a `{owner}/{repo}@{ref}` or `{owner}/{repo}/{path}@{ref}` specification into its owner, repository and ref.
// The problem is empty when the specification has one of those shapes, in which case any of the three parts may still be empty.

func SplitAction(spec string) (owner, repo, ref, problem string) {
	s := spec
	idx := strings.IndexRune(s, '@')
	if idx == -1 {
		return "", "", "", "ref is missing"
	}
	ref = s[idx+1:]
	s = s[:idx] // remove {ref}

	idx = strings.IndexRune(s, '/')
	if idx == -1 {
		return "", "", "", "owner is missing"
	}

	owner = s[:idx]
	s = s[idx+1:] // eat {owner}

	repo = s
	if idx := strings.IndexRune(s, '/'); idx >= 0 {
		repo = s[:idx]
	}

	return owner, repo, ref, ""
}
func SelfRepositoryLocal(spec string) (string, bool) {
	p, ok := strings.CutPrefix(spec, "$/")
	if !ok || strings.ContainsRune(p, '@') {
		return "", false
	}
	p = strings.TrimLeft(p, "/")
	return "./" + p, true
}
func LocalWorkflow(u string) (string, bool) {
	if strings.HasPrefix(u, "$/") {
		s, ok := SelfRepositoryLocal(u)
		if !ok {
			return "", false
		}
		u = s
	} else if !strings.HasPrefix(u, "./") {
		return "", false
	}
	path := strings.TrimPrefix(u, "./")

	// Cannot contain a ref
	idx := strings.IndexRune(path, '@')
	if idx > 0 {
		return "", false
	}

	return u, len(path) > 0
}
func WorkflowRef(u string) (string, bool) {
	// Repo reference must start with owner
	if strings.HasPrefix(u, ".") || strings.HasPrefix(u, "$") {
		return "", false
	}

	idx := strings.IndexRune(u, '/')
	if idx <= 0 {
		return "", false
	}
	u = u[idx+1:] // Eat owner

	idx = strings.IndexRune(u, '/')
	if idx <= 0 {
		return "", false
	}
	u = u[idx+1:] // Eat repo

	idx = strings.IndexRune(u, '@')
	if idx <= 0 {
		return "", false
	}
	u = u[idx+1:] // Eat workflow path

	if len(u) == 0 {
		return "", false
	}
	return u, true
}
func CacheAction(uses string) string {
	name, ref, ok := strings.Cut(uses, "@")
	if !ok || ref == "" || exprlang.ContainsExpression(uses) {
		return ""
	}
	owner, rest, ok := strings.Cut(name, "/")
	if !ok || !strings.EqualFold(owner, "actions") {
		return ""
	}
	repo, path, hasPath := strings.Cut(rest, "/")
	if !strings.EqualFold(repo, "cache") || hasPath && path == "" {
		return ""
	}
	// Repository names are case-insensitive; action subpaths address files in the downloaded repository and retain their case.
	if path == "" || path == "save" || path == "restore" {
		return name
	}
	return ""
}

// SplitRef separates an action name from its ref or Docker digest.
func SplitRef(spec string) (name, ref string) {
	name, ref, _ = strings.Cut(spec, "@")
	return
}
