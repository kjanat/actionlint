package configsource

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// IsRemoteAddress reports whether name uses the HTTPS configuration source scheme.
// Normalize must still validate the complete address before it is read.
func IsRemoteAddress(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "https:")
}

// NormalizeAddress validates an HTTPS address or returns an absolute local filename.
// Queries belong to remote identity; fragments and credentials are forbidden.
func NormalizeAddress(name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsRune(name, 0) {
		return "", errors.New("config source requires a nonempty address without NUL")
	}
	if hasScheme(name) {
		u, err := url.Parse(name)
		if err != nil {
			return "", fmt.Errorf("invalid config source %q: %w", name, err)
		}
		if u.Scheme != "https" || u.Hostname() == "" || u.Opaque != "" {
			return "", fmt.Errorf("config source %q requires a local file or an absolute HTTPS URL", name)
		}
		if u.User != nil || strings.Contains(name, "#") || strings.ContainsAny(name, "\\\r\n") {
			return "", fmt.Errorf("HTTPS config source %q cannot contain credentials, fragments or backslashes", name)
		}
		u.Host = strings.ToLower(u.Host)
		return u.String(), nil
	}
	return filepath.Abs(name)
}

// ResolveAddress interprets a dependency relative to its declaring source. Remote
// sources resolve URL references; they cannot reach local filesystem addresses.
func ResolveAddress(origin, reference string) (string, error) {
	if strings.TrimSpace(reference) == "" || strings.ContainsRune(reference, 0) {
		return "", errors.New("extends requires a nonempty source address without NUL")
	}
	if IsRemoteAddress(origin) {
		if strings.Contains(reference, "#") {
			return "", errors.New("extends URL references cannot contain fragments")
		}
		base, err := url.Parse(origin)
		if err != nil {
			return "", err
		}
		next, err := url.Parse(reference)
		if err != nil {
			return "", err
		}
		if next.IsAbs() {
			if _, err := NormalizeAddress(reference); err != nil {
				return "", err
			}
			return reference, nil
		}
		return NormalizeAddress(base.ResolveReference(next).String())
	}
	if hasScheme(reference) {
		if _, err := NormalizeAddress(reference); err != nil {
			return "", err
		}
		return reference, nil
	}
	if !filepath.IsAbs(reference) {
		reference = filepath.Join(filepath.Dir(origin), reference)
	}
	return NormalizeAddress(reference)
}

func hasScheme(name string) bool {
	return strings.Contains(name, ":") && filepath.VolumeName(name) == ""
}

// AddressExtension selects a remote format from the URL path, independently of queries.
func AddressExtension(name string) string {
	if IsRemoteAddress(name) {
		if u, err := url.Parse(name); err == nil {
			return path.Ext(u.Path)
		}
	}
	return filepath.Ext(name)
}
