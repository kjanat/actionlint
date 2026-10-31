// Package address resolves configuration source addresses without acquiring bytes.
package address

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// Remote reports whether name uses the HTTPS configuration source scheme.
// Normalize must still validate the complete address before it is read.
func Remote(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "https:")
}

// Normalize validates an HTTPS address or returns an absolute local filename.
// Queries belong to remote identity; fragments and credentials are forbidden.
func Normalize(name string) (string, error) {
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

// Resolve interprets a dependency relative to its declaring source. Remote
// sources resolve URL references; they cannot reach local filesystem addresses.
func Resolve(origin, reference string) (string, error) {
	if strings.TrimSpace(reference) == "" || strings.ContainsRune(reference, 0) {
		return "", errors.New("extends requires a nonempty source address without NUL")
	}
	if Remote(origin) {
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
			if _, err := Normalize(reference); err != nil {
				return "", err
			}
			return reference, nil
		}
		return Normalize(base.ResolveReference(next).String())
	}
	if hasScheme(reference) {
		if _, err := Normalize(reference); err != nil {
			return "", err
		}
		return reference, nil
	}
	if !filepath.IsAbs(reference) {
		reference = filepath.Join(filepath.Dir(origin), reference)
	}
	return Normalize(reference)
}

func hasScheme(name string) bool {
	return strings.Contains(name, ":") && filepath.VolumeName(name) == ""
}

// Extension selects a remote format from the URL path, independently of queries.
func Extension(name string) string {
	if Remote(name) {
		if u, err := url.Parse(name); err == nil {
			return path.Ext(u.Path)
		}
	}
	return filepath.Ext(name)
}
