package finding

import "actionlint.kjanat.dev/v2/internal/base/sourcetext"

// MetadataKey has a zero Start when the finding has no source position.
type MetadataKey struct {
	Path, Message string
	Start         sourcetext.Position
}

type MetadataIdentity struct {
	Key         MetadataKey
	Visit       uint64
	Declaration bool
}
