// Package callreferences interprets action and reusable-workflow references.
package callreferences

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
)

// ParseRepositoryAction parses a step's {owner}/{repo}[/{path}]@{ref} action
// reference the way the runner does: spec must contain exactly one "@", the
// part before it splits on slashes and backslashes with empty segments dropped, and the
// owner, repository and ref must not be empty. Segments after the repository
// form Subpath, joined with "/". problem says why spec has another shape; it
// is empty when spec parses.
// https://github.com/actions/runner/blob/main/src/Sdk/DTPipelines/Pipelines/ObjectTemplating/PipelineTemplateConverter.cs
func ParseRepositoryAction(spec string) (target RepositoryTarget, problem string) {
	location, ref, found := strings.Cut(spec, "@")
	switch {
	case !found:
		return RepositoryTarget{}, "ref is missing"
	case strings.Contains(ref, "@"):
		return RepositoryTarget{}, "\"@\" appears more than once"
	case !strings.ContainsAny(location, "/\\"):
		return RepositoryTarget{}, "owner is missing"
	}
	segments := strings.FieldsFunc(location, func(r rune) bool { return r == '/' || r == '\\' })
	if len(segments) < 2 || ref == "" {
		return RepositoryTarget{}, "owner and repo and ref should not be empty"
	}
	return RepositoryTarget{Owner: segments[0], Repo: segments[1], Subpath: strings.Join(segments[2:], "/"), Ref: ref}, ""
}

func SelfRepositoryLocal(spec string) (string, bool) {
	p, ok := strings.CutPrefix(spec, selfRepositoryPrefix)
	if !ok || strings.ContainsRune(p, '@') {
		return "", false
	}
	p = strings.TrimLeft(p, "/")
	return "./" + p, true
}

func LocalWorkflow(u string) (string, bool) {
	switch FormOf(u) {
	case FormSelfRepository:
		s, ok := SelfRepositoryLocal(u)
		if !ok {
			return "", false
		}
		u = s
	case FormWorkspace:
	case FormRepository, FormContainer, FormBuiltin:
		return "", false
	}
	path := strings.TrimPrefix(u, workspacePrefix)

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
	if !ok || ref == "" || syntax.ContainsExpression(uses) {
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
	// Repository names are case-insensitive; subpaths use the repository filesystem's spelling.
	if path == "" || path == "save" || path == "restore" {
		return name
	}
	return ""
}

// SplitRef separates an action name from its ref or Docker digest.
func SplitRef(spec string) (name, ref string) {
	name, ref, _ = strings.Cut(spec, "@")
	return name, ref
}
