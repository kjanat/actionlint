package pluginreference

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

type ModuleLocation struct {
	// Endpoint is the URI without its fragment, or nil for a filesystem path.
	Endpoint *url.URL
	// Local is empty for an http, https or sha256 URI.
	Local string
	// Pin is the lowercase hex SHA256 digest from #sha256=<hex> or a sha256 URI.
	Pin string
}

// ParseModuleLocation treats only explicit :// locations as URIs. It refuses
// credentials in URIs, malformed digest pins and other schemes, including oci.
func ParseModuleLocation(source string) (ModuleLocation, error) {
	if !strings.Contains(source, "://") {
		if source == "" {
			return ModuleLocation{}, errors.New("empty plugin location")
		}
		return ModuleLocation{Local: source}, nil
	}
	u, err := url.Parse(source)
	if err != nil {
		return ModuleLocation{}, errors.New("invalid plugin URI")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.User != nil {
		return ModuleLocation{}, errors.New("plugin URIs cannot contain credentials")
	}
	pin := ""
	if u.Fragment != "" {
		digest, prefixed := strings.CutPrefix(u.Fragment, "sha256=")
		normalized, ok := NormalizeDigest(digest)
		if !prefixed || !ok {
			return ModuleLocation{}, errors.New("plugin URI fragment must be sha256=<64 hex digits>")
		}
		pin = normalized
	}
	u.Fragment, u.RawFragment = "", ""
	switch u.Scheme {
	case "sha256":
		if CheckDigest(u.Host) != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || pin != "" {
			return ModuleLocation{}, errors.New("invalid cached artifact digest")
		}
		return ModuleLocation{Endpoint: u, Pin: u.Host}, nil
	case "http", "https":
		if u.Host == "" {
			return ModuleLocation{}, errors.New("plugin URL requires a host")
		}
		return ModuleLocation{Endpoint: u, Pin: pin}, nil
	case "file":
		if (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) || u.RawQuery != "" {
			return ModuleLocation{}, errors.New("file plugin URI requires a local absolute path without a query")
		}
		path := u.Path
		if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		path = filepath.FromSlash(path)
		if !filepath.IsAbs(path) {
			return ModuleLocation{}, errors.New("file plugin URI requires an absolute path")
		}
		return ModuleLocation{Endpoint: u, Local: path, Pin: pin}, nil
	default:
		return ModuleLocation{}, errors.New("plugin URI scheme must be file, http, https or oci")
	}
}

// File roots a relative filesystem path in directory and returns "" for a
// remote location.
func (l ModuleLocation) File(directory string) string {
	if l.Endpoint != nil {
		return l.Local
	}
	if !filepath.IsAbs(l.Local) {
		return filepath.Join(directory, l.Local)
	}
	return filepath.Clean(l.Local)
}
