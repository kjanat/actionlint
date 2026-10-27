package mapping

import "actionlint.kjanat.dev/v2/internal/workflow/value"

type KeyState uint8

const (
	UnknownKey KeyState = iota
	KnownKey
	InvalidKey
)

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
