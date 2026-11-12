package references

import "strings"

// RepositoryTarget is the repository, optional subpath and revision in uses.
// It does not identify a host or establish that the target exists.
type RepositoryTarget struct{ Owner, Repo, Subpath, Ref string }

// ParseRepositoryUses accepts the default-host owner/repo[/path]@ref form.
// Workspace, self-repository, container and URL references use other forms.
func ParseRepositoryUses(raw string) (RepositoryTarget, bool) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.Contains(raw, "${{") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "$/") || strings.HasPrefix(raw, "self:") {
		return RepositoryTarget{}, false
	}
	at := strings.LastIndexByte(raw, '@')
	if at < 0 || at == len(raw)-1 {
		return RepositoryTarget{}, false
	}
	target, ok := ParseRepositoryPath(raw[:at])
	if !ok {
		return RepositoryTarget{}, false
	}
	target.Ref = raw[at+1:]
	return target, true
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
