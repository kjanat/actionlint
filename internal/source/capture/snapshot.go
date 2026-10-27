package capture

import (
	"bytes"
	"slices"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/sourceid"
	"actionlint.kjanat.dev/v2/source"
)

type captured struct {
	revision source.Revision
	model    source.Coordinates
	content  []byte
	utf8     bool
}

// Snapshot is an immutable Store handle. Its frozen aliases are diagnostic-only;
// publication uses the complete, deterministic table returned by Sources.
type Snapshot struct {
	store   *Store
	value   *captured
	aliases []string
}

func (s Snapshot) Valid() bool { return s.store != nil && s.value != nil }

func (s Snapshot) Revision() source.Revision {
	if s.value == nil {
		return source.Revision{}
	}
	return s.value.revision
}

func (s Snapshot) Coordinates() source.Coordinates {
	if s.value == nil {
		return ""
	}
	return s.value.model
}

// CoordinatesAvailable is independent of whether the captured digest is known.
func (s Snapshot) CoordinatesAvailable() bool { return s.value != nil && s.value.utf8 }

func (s Snapshot) Bytes() []byte {
	if s.value == nil {
		return nil
	}
	return bytes.Clone(s.value.content)
}

func (s Snapshot) Aliases() []string { return slices.Clone(s.aliases) }

func capture(document string, model source.Coordinates, content []byte) (*captured, error) {
	content = bytes.Clone(content)
	revision, err := source.NewRevision(document, sourceid.Digest(content))
	if err != nil {
		return nil, err
	}
	return &captured{revision, model, content, utf8.Valid(content)}, nil
}
