// Package bundled holds the tool providers shipped inside actionlint. Each
// directory is one provider: manifest.json and the transform programs the
// manifest references by sha256 digest. The directory name is the provider's
// rule name.
//
// The package is data only and imports no actionlint package, so the rule
// catalog and configuration decoding can read the manifests without
// depending on any tool host. provider.Bundled parses and validates them.
package bundled

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"path"
	"strings"
	"sync"
)

//go:embed */manifest.json */*.jq
var files embed.FS

// FS returns the bundled provider directories.
func FS() fs.FS { return files }

// Names lists the bundled providers' rule names in directory order.
func Names() []string {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

// Manifest returns the unparsed manifest of the provider named name.
func Manifest(name string) ([]byte, bool) {
	if !fs.ValidPath(name) || strings.Contains(name, "/") {
		return nil, false
	}
	data, err := files.ReadFile(path.Join(name, "manifest.json"))
	return data, err == nil
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
	for _, name := range Names() {
		entries, err := files.ReadDir(name)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".jq") {
				continue
			}
			data, err := files.ReadFile(path.Join(name, entry.Name()))
			if err != nil {
				continue
			}
			sum := sha256.Sum256(data)
			index["sha256:"+hex.EncodeToString(sum[:])] = data
		}
	}
	return index
})
