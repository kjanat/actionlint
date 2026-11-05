package json

import (
	"errors"
	"strings"
	"unicode"
)

type MemberCollision struct {
	First, Second string
	Offset        int64
}

// OrdinalIgnoreCaseKey does not equate non-ASCII characters with ASCII letters,
// even when Unicode uppercasing maps dotless i or long s to I or S.
func OrdinalIgnoreCaseKey(s string) string {
	return strings.Map(func(r rune) rune {
		upper := unicode.ToUpper(r)
		if r >= 128 && upper < 128 {
			return r
		}
		return upper
	}, s)
}

// JSONMemberCollisions finds case-insensitive duplicate keys in the first value
// read by fromJSON. Invalid input is diagnosed by the shared reader instead.
func JSONMemberCollisions(source string) []MemberCollision {
	_, collisions, err := Read(source)
	if err != nil && !errors.Is(err, ErrUnsupportedValue) {
		return nil
	}
	return collisions
}
