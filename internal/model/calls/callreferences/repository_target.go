package callreferences

import "strings"

// RepositoryTarget is the repository, optional subpath and revision in uses.
// It does not identify a host or establish that the target exists.
type RepositoryTarget struct{ Owner, Repo, Subpath, Ref string }

// ParseRepositoryUses accepts the default-host owner/repo[/path]@ref form.
// Workspace, self-repository, container, builtin, self-host and URL references
// use other forms.
func ParseRepositoryUses(raw string) (RepositoryTarget, bool) {
	reference, ok := Classify(raw).(RepositoryReference)
	if !ok || reference.HostSource != HostSourceDefault {
		return RepositoryTarget{}, false
	}
	return RepositoryTarget{Owner: reference.Owner, Repo: reference.Repo, Subpath: reference.Subpath, Ref: reference.Ref}, true
}

// ParseRepositoryPath validates owner/repo[/path] without interpreting its host.
func ParseRepositoryPath(location string) (RepositoryTarget, bool) {
	parts := strings.Split(location, "/")
	if len(parts) < 2 || strings.ContainsAny(location, "@: \\\t\r\n") {
		return RepositoryTarget{}, false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return RepositoryTarget{}, false
		}
	}
	return RepositoryTarget{Owner: parts[0], Repo: parts[1], Subpath: strings.Join(parts[2:], "/")}, true
}
