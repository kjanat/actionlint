package finding

import "actionlint.kjanat.dev/v2/internal/base/sourcetext"

// MetadataKey identifies an action metadata finding by document, message and
// start. Start is zero when the finding has no source position.
type MetadataKey struct {
	Path, Message string
	Start         sourcetext.Position
}

type MetadataIdentity struct {
	Key         MetadataKey
	Visit       uint64
	Declaration bool
}
