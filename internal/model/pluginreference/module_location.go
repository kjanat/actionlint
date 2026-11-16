package pluginreference

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// ModuleLocation is a parsed WebAssembly module or tool provider package
// location that is not an oci:// reference: a filesystem path, or a file,
// http, https or sha256 URI. Parsing reads no files and makes no requests.
type ModuleLocation struct {
	// Endpoint is the URI without its fragment. It is nil for a filesystem
	// path.
	Endpoint *url.URL
	// Local is the filesystem path the location names. It is empty for an
	// http, https or sha256 URI.
	Local string
	// Pin is the lowercase hex SHA256 digest the location pins with
	// #sha256=<hex>, or the digest a sha256 URI names. It is empty when the
	// location pins nothing.
	Pin string
}

// ParseModuleLocation parses source as a filesystem path or a file, http,
// https or sha256 URI. Only explicit :// locations are URIs, so drive paths
// and colons in ordinary filenames stay paths on every host. It refuses
// credentials in URIs, malformed digest pins and other schemes, including
// oci.
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
		pin = strings.TrimPrefix(u.Fragment, "sha256=")
		decoded, err := hex.DecodeString(pin)
		if !strings.HasPrefix(u.Fragment, "sha256=") || err != nil || len(decoded) != sha256.Size {
			return ModuleLocation{}, errors.New("plugin URI fragment must be sha256=<64 hex digits>")
		}
		pin = strings.ToLower(pin)
	}
	u.Fragment, u.RawFragment = "", ""
	switch u.Scheme {
	case "sha256":
		decoded, err := hex.DecodeString(u.Host)
		if err != nil || len(decoded) != sha256.Size || u.Host != strings.ToLower(u.Host) || u.Path != "" || u.RawQuery != "" || u.ForceQuery || pin != "" {
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

// File returns the local file the location names. A relative filesystem
// path is rooted in directory and a filesystem path is cleaned; a file URI's
// path is returned as parsed. File returns "" for a remote location.
func (l ModuleLocation) File(directory string) string {
	if l.Endpoint != nil {
		return l.Local
	}
	if !filepath.IsAbs(l.Local) {
		return filepath.Join(directory, l.Local)
	}
	return filepath.Clean(l.Local)
}
