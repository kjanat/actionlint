package configsource

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/endpoint"
)

// IsRemoteAddress leaves validation to NormalizeAddress.
func IsRemoteAddress(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "https:")
}

// NormalizeAddress keeps queries as part of a remote address's identity.
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
		authority, err := endpoint.Of(u)
		if err != nil {
			return "", fmt.Errorf("config source %q requires a local file or an absolute HTTPS URL", name)
		}
		u.Host = authority.URLHost()
		return u.String(), nil
	}
	return filepath.Abs(name)
}

// ResolveAddress never lets a remote source reach a local filesystem address.
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

// DisplayAddress drops a remote address's query, which may carry a credential.
func DisplayAddress(name string) string {
	if !IsRemoteAddress(name) {
		return name
	}
	display, _, _ := strings.Cut(name, "?")
	return display
}

func AddressExtension(name string) string {
	if IsRemoteAddress(name) {
		if u, err := url.Parse(name); err == nil {
			return path.Ext(u.Path)
		}
	}
	return filepath.Ext(name)
}
