package callreferences

import (
	"net/url"
	"path"
	"strings"
)

// RepositoryURL offers a shorthand only when removing its prefix keeps the repository, action directory and ref on GitHub.com.
type RepositoryURL struct {
	host, shorthand string
	prefixLength    int
}

func (r RepositoryURL) Host() string      { return r.host }
func (r RepositoryURL) Shorthand() string { return r.shorthand }
func (r RepositoryURL) PrefixLength() int { return r.prefixLength }

// ParseRepositoryURL excludes Docker registry references and relative paths.
func ParseRepositoryURL(spec string) (RepositoryURL, bool) {
	u, err := url.Parse(spec)
	if err != nil || !u.IsAbs() || u.Host == "" || strings.EqualFold(u.Scheme, "docker") {
		return RepositoryURL{}, false
	}
	r := RepositoryURL{host: u.Hostname()}
	if !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(spec, "#") || strings.Contains(u.EscapedPath(), "%") {
		return r, true
	}
	shorthand := strings.TrimPrefix(u.Path, "/")
	location, _, _ := strings.Cut(shorthand, "@")
	if _, problem := ParseRepositoryAction(shorthand); problem != "" || path.Clean(location) != location || strings.ContainsAny(shorthand, "\\ \t\r\n") {
		return r, true
	}
	r.shorthand = shorthand
	r.prefixLength = len(spec) - len(shorthand)
	return r, true
}
