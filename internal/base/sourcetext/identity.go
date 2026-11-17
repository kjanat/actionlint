package sourcetext

import (
	"path/filepath"
	"strconv"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
)

func DocumentID(path string) string {
	if path == "<stdin>" || RemoteAddress(path) {
		return path
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return filepath.ToSlash(filepath.Clean(path))
}

// ReferenceIndex assigns content-derived identities to anonymous siblings and key-derived
// identities to named nodes. Reordering distinct anonymous siblings preserves IDs;
// indistinguishable duplicates are disambiguated by occurrence.
type ReferenceIndex struct {
	document, revision string
	occurrences        map[string]int
}

func NewReferenceIndex(path string, content []byte) *ReferenceIndex {
	return &ReferenceIndex{DocumentID(path), sha256hex.Digest(content), map[string]int{}}
}

func (i *ReferenceIndex) Ref(owner, key, content, address string) Reference {
	identity := owner + "\x00" + key
	if key == "" {
		identity = owner + "\x00anonymous\x00" + sha256hex.Digest([]byte(content))
	}
	occurrence := i.occurrences[identity]
	i.occurrences[identity]++
	node := sha256hex.Digest([]byte(i.document + "\x00" + identity + "\x00" + strconv.Itoa(occurrence)))
	return Reference{Document: i.document, Revision: i.revision, Node: node, Address: address}
}
