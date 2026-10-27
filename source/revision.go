package source

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// Revision identifies the exact bytes captured for a canonical document.
// Its zero value is invalid. Construction does not resolve or read documents;
// the captured-source owner supplies the canonical identity.
type Revision struct {
	document string
	digest   string
}

// NewRevision validates an identity and a lowercase SHA-256 digest.
func NewRevision(document, digest string) (Revision, error) {
	if document == "" || strings.ContainsRune(document, 0) {
		return Revision{}, errors.New("source revision requires a document identity")
	}
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		return Revision{}, errors.New("source revision requires a lowercase SHA-256 digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return Revision{}, errors.New("source revision requires a lowercase SHA-256 digest")
	}
	return Revision{document: document, digest: digest}, nil
}

func (r Revision) Document() string { return r.document }
func (r Revision) Digest() string   { return r.digest }
func (r Revision) Valid() bool      { return r.document != "" && r.digest != "" }

type revisionJSON struct {
	Document string `json:"document"`
	Digest   string `json:"digest"`
}

// MarshalJSON encodes a validated value identity; zero revisions are invalid.
func (r Revision) MarshalJSON() ([]byte, error) {
	validated, err := NewRevision(r.document, r.digest)
	if err != nil {
		return nil, err
	}
	return json.Marshal(revisionJSON{validated.document, validated.digest})
}

// UnmarshalJSON validates before replacing the receiver.
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
