package capture

import (
	"bytes"
	"slices"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/sha256hex"
)

type captured struct {
	revision sourcetext.Revision
	model    sourcetext.Coordinates
	content  []byte
	utf8     bool
}

// CapturedDocument is an immutable Store handle. Its frozen aliases are diagnostic-only;
// publication uses the complete, deterministic table returned by Sources.
type CapturedDocument struct {
	store   *Store
	value   *captured
	aliases []string
}

func (s CapturedDocument) Valid() bool { return s.store != nil && s.value != nil }

func (s CapturedDocument) Revision() sourcetext.Revision {
	if s.value == nil {
		return sourcetext.Revision{}
	}
	return s.value.revision
}

func (s CapturedDocument) Coordinates() sourcetext.Coordinates {
	if s.value == nil {
		return ""
	}
	return s.value.model
}

// CoordinatesAvailable is independent of whether the captured digest is known.
func (s CapturedDocument) CoordinatesAvailable() bool { return s.value != nil && s.value.utf8 }

func (s CapturedDocument) Bytes() []byte {
	if s.value == nil {
		return nil
	}
	return bytes.Clone(s.value.content)
}

func (s CapturedDocument) Aliases() []string { return slices.Clone(s.aliases) }

func capture(document string, model sourcetext.Coordinates, content []byte) (*captured, error) {
	content = bytes.Clone(content)
	revision, err := sourcetext.NewRevision(document, sha256hex.Digest(content))
	if err != nil {
		return nil, err
	}
	return &captured{revision, model, content, utf8.Valid(content)}, nil
}
