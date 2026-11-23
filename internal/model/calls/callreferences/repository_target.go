package callreferences

import "strings"

// RepositoryTarget identifies no host and does not prove the target exists.
type RepositoryTarget struct{ Owner, Repo, Subpath, Ref string }

func (t RepositoryTarget) Location() string {
	if t.Subpath == "" {
		return t.Owner + "/" + t.Repo
	}
	return t.Owner + "/" + t.Repo + "/" + t.Subpath
}

// ParseRepositoryUses accepts the default-host owner/repo[/path]@ref form.
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
