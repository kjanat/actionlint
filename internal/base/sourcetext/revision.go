package sourcetext

import (
	"encoding/json"
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sha256hex"
)

// Revision identifies the exact bytes captured for a canonical document.
// Its zero value is invalid.
type Revision struct {
	document string
	digest   string
}

func NewRevision(document, digest string) (Revision, error) {
	if document == "" || strings.ContainsRune(document, 0) {
		return Revision{}, errors.New("source revision requires a document identity")
	}
	if !sha256hex.IsDigest(digest) {
		return Revision{}, errors.New("source revision requires a lowercase SHA-256 digest")
	}
	return Revision{document: document, digest: digest}, nil
}

func (r Revision) Document() string { return r.document }
func (r Revision) Digest() string   { return r.digest }
func (r Revision) Valid() bool      { return r.document != "" && r.digest != "" }

func (r Revision) Equal(other Revision) bool { return r == other }

type revisionJSON struct {
	Document string `json:"document"`
	Digest   string `json:"digest"`
}

func (r Revision) MarshalJSON() ([]byte, error) {
	validated, err := NewRevision(r.document, r.digest)
	if err != nil {
		return nil, err
	}
	return json.Marshal(revisionJSON{validated.document, validated.digest})
}

func (r *Revision) UnmarshalJSON(data []byte) error {
	var value revisionJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	revision, err := NewRevision(value.Document, value.Digest)
	if err != nil {
		return err
	}
	*r = revision
	return nil
}
