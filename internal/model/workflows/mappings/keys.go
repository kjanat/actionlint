package mappings

import "actionlint.kjanat.dev/v2/internal/model/contexts/value"

type KeyState uint8

const (
	UnknownKey KeyState = iota
	KnownKey
	InvalidKey
)

func (s KeyState) String() string {
	switch s {
	case KnownKey:
		return "known"
	case InvalidKey:
		return "invalid"
	default:
		return "unknown"
	}
}

func ResolveKey(source value.Value) (string, KeyState) {
	if source.State() == value.Dynamic || source.State() == value.Invalid || source.State() == value.Missing {
		return "", UnknownKey
	}
	name, ok := source.ScalarString()
	if !ok || name == "" {
		return "", InvalidKey
	}
	return name, KnownKey
}
