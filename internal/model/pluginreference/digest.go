package pluginreference

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
)

// NormalizeDigest accepts a SHA256 digest written as 64 hex digits in either
// case and returns it in lowercase.
func NormalizeDigest(digest string) (string, bool) {
	normalized := strings.ToLower(digest)
	if !sha256hex.IsDigest(normalized) {
		return "", false
	}
	return normalized, true
}

// CheckDigest accepts a SHA256 digest written as 64 lowercase hex digits, the
// canonical form of OCI digests and sha256:// cache addresses.
func CheckDigest(digest string) error {
	if !sha256hex.IsDigest(digest) {
		return fmt.Errorf("invalid artifact digest %q: want 64 lowercase hex digits", digest)
	}
	return nil
}
