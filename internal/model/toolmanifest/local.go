package toolmanifest

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// Programs resolves a transform digest "sha256:<hex>" to its program.
type Programs func(digest string) ([]byte, bool)

// IsLocalManifest reports whether a plugin location names a local provider
// manifest rather than a WebAssembly module.
func IsLocalManifest(location string) bool {
	return !strings.Contains(location, "://") && strings.EqualFold(filepath.Ext(location), ".json")
}

// LoadPrograms reads every transform program manifest names by digest, such
// as "sha256:<hex>", with read, and returns them as Programs. It reads them
// now, so a later check does not depend on read's context.
func LoadPrograms(manifest Manifest, read func(digest string) ([]byte, error)) (Programs, error) {
	programs := map[string][]byte{}
	for _, transform := range manifest.Transforms() {
		if transform.Digest == "" {
			continue
		}
		program, err := read(transform.Digest)
		if err != nil {
			return nil, fmt.Errorf("tool provider %s: transform %s: %w", manifest.Rule.Name, transform.Digest, err)
		}
		programs[transform.Digest] = program
	}
	return func(digest string) ([]byte, bool) {
		program, ok := programs[digest]
		return bytes.Clone(program), ok
	}, nil
}
