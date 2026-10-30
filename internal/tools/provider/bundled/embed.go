// Package bundled holds the tool providers shipped inside actionlint. Each
// directory is one provider: manifest.json and the transform programs the
// manifest references by sha256 digest. The directory name is the provider's
// rule name.
package bundled

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

//go:embed */manifest.json */*.jq
var files embed.FS

// FS returns the bundled provider directories.
func FS() fs.FS { return files }

// Manifests parses every bundled manifest in directory order. Each manifest's
// rule name must equal its directory and each transform digest must name a
// bundled program.
func Manifests() ([]provider.Manifest, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	manifests := make([]provider.Manifest, 0, len(entries))
	for _, entry := range entries {
		data, err := files.ReadFile(path.Join(entry.Name(), "manifest.json"))
		if err != nil {
			return nil, err
		}
		manifest, err := provider.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("bundled provider %s: %w", entry.Name(), err)
		}
		if manifest.Rule.Name != entry.Name() {
			return nil, fmt.Errorf("bundled provider %s declares rule %q", entry.Name(), manifest.Rule.Name)
		}
		for _, transform := range transforms(manifest) {
			if transform.Digest == "" {
				continue
			}
			if _, ok := Program(transform.Digest); !ok {
				return nil, fmt.Errorf("bundled provider %s references missing program %s", entry.Name(), transform.Digest)
			}
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

// Program returns the bundled transform program whose content has the digest
// "sha256:<hex>".
func Program(digest string) ([]byte, bool) {
	program, ok := programs()[digest]
	return program, ok
}

// programs indexes every bundled program by content digest.
var programs = sync.OnceValue(func() map[string][]byte {
	index := make(map[string][]byte)
	entries, err := files.ReadDir(".")
	if err != nil {
		return index
	}
	for _, entry := range entries {
		names, err := files.ReadDir(entry.Name())
		if err != nil {
			continue
		}
		for _, name := range names {
			if !strings.HasSuffix(name.Name(), ".jq") {
				continue
			}
			data, err := files.ReadFile(path.Join(entry.Name(), name.Name()))
			if err != nil {
				continue
			}
			sum := sha256.Sum256(data)
			index["sha256:"+hex.EncodeToString(sum[:])] = data
		}
	}
	return index
})

// transforms lists the transforms a manifest references.
func transforms(manifest provider.Manifest) []*provider.Transform {
	if manifest.Process == nil {
		return nil
	}
	var found []*provider.Transform
	for _, transform := range []*provider.Transform{manifest.Process.Config.Transform, manifest.Process.Output.Transform} {
		if transform != nil {
			found = append(found, transform)
		}
	}
	return found
}
