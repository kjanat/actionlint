package definition

import "actionlint.kjanat.dev/v2/internal/base/sourcetext"

type CacheModeKind uint8

const (
	CacheModeInvalid CacheModeKind = iota
	CacheModeNone
	CacheModeRead
	CacheModeWrite
	CacheModeWriteOnly
)

func (k CacheModeKind) String() string {
	switch k {
	case CacheModeNone:
		return "none"
	case CacheModeRead:
		return "read"
	case CacheModeWrite:
		return "write"
	case CacheModeWriteOnly:
		return "write-only"
	case CacheModeInvalid:
	}
	return "invalid"
}

// CacheMode is nil when the declaration is omitted.
type CacheMode struct {
	Kind CacheModeKind
	Pos  *sourcetext.Position
}

func (m *CacheMode) Capabilities() (uint8, bool) {
	if m == nil {
		return 0, false
	}
	return m.Kind.Capabilities()
}

func (k CacheModeKind) Capabilities() (uint8, bool) {
	switch k {
	case CacheModeNone:
		return 0, true
	case CacheModeRead:
		return 1, true
	case CacheModeWriteOnly:
		return 2, true
	case CacheModeWrite:
		return 3, true
	case CacheModeInvalid:
	}
	return 0, false
}

func EffectiveCacheMode(workflow, job *CacheMode) *CacheMode {
	if job != nil {
		return job
	}
	return workflow
}
