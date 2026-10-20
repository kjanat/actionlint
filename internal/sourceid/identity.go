// Package sourceid assigns document and syntax-node identities.
package sourceid

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strconv"

	"actionlint.kjanat.dev/v2/source"
)

func Digest(content []byte) string { sum := sha256.Sum256(content); return hex.EncodeToString(sum[:]) }
func Document(path string) string {
	if path == "<stdin>" {
		return path
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return filepath.ToSlash(filepath.Clean(path))
}

// Index assigns content-derived identities to anonymous siblings and key-derived
// identities to named nodes. Reordering distinct anonymous siblings preserves IDs;
// indistinguishable duplicates are disambiguated by occurrence.
type Index struct {
	document, revision string
	occurrences        map[string]int
}

func New(path string, content []byte) *Index {
	return &Index{Document(path), Digest(content), map[string]int{}}
}
func (i *Index) Ref(owner, key, content, address string) source.Reference {
	identity := owner + "\x00" + key
	if key == "" {
		identity = owner + "\x00anonymous\x00" + Digest([]byte(content))
	}
	occurrence := i.occurrences[identity]
	i.occurrences[identity]++
	node := Digest([]byte(i.document + "\x00" + identity + "\x00" + strconv.Itoa(occurrence)))
	return source.Reference{Document: i.document, Revision: i.revision, Node: node, Address: address}
}
