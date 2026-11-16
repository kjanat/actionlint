package mappings

import "actionlint.kjanat.dev/v2/internal/model/contexts/value"

// KeyState distinguishes a statically known mapping key from one whose name
// depends on runtime values and from one that can never name a key.
type KeyState uint8

const (
	UnknownKey KeyState = iota
	KnownKey
	InvalidKey
)

// String returns the key state's name in the plugin contract.
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

// ResolveKey applies workflow mapping key coercion without reparsing evaluated data.
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
