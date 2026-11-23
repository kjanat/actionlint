package results

import (
	"net/url"
	"strings"
)

// DependencyAddress identifies a GitHub dependency source without its pin.
// Unknown URL layouts remain separate sources.
func DependencyAddress(address string) (source, pin string, ok bool) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.RawQuery != "" {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 5)
	if len(parts) != 5 || parts[0] == "" || parts[1] == "" || parts[2] != "blob" || parts[3] == "" || parts[4] == "" {
		return "", "", false
	}
	return parts[0] + "/" + parts[1] + "/" + parts[4], parts[3], true
}

const IdenticalDependencyLocation = "Identical dependency finding at another pin."
