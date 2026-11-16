package pluginreference

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// NormalizeDigest accepts a SHA256 digest written as 64 hex digits in either
// case and returns it in lowercase. URL pins (#sha256=<hex>) and candidate
// digests use it; content addresses, which must already be canonical, use
// CheckDigest.
func NormalizeDigest(digest string) (string, bool) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", false
	}
	return strings.ToLower(digest), true
}

// CheckDigest accepts a SHA256 digest written as 64 lowercase hex digits, the
// canonical form of OCI digests and sha256:// cache addresses.
func CheckDigest(digest string) error {
	if normalized, ok := NormalizeDigest(digest); !ok || normalized != digest {
		return fmt.Errorf("invalid artifact digest %q: want 64 lowercase hex digits", digest)
	}
	return nil
}
