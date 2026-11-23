// Package sha256hex formats and validates SHA-256 digests as lowercase
// hexadecimal text.
package sha256hex

import (
	"crypto/sha256"
	"encoding/hex"
)

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func IsDigest(text string) bool {
	if len(text) != 2*sha256.Size {
		return false
	}
	for i := range len(text) {
		if c := text[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
