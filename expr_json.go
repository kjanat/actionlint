package actionlint

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
)

type jsonMemberCollision struct {
	first, second string
	offset        int64
}

// OrdinalIgnoreCase does not equate non-ASCII characters with ASCII letters,
// even when Unicode uppercasing maps dotless i or long s to I or S.
func ordinalIgnoreCaseKey(s string) string {
	return strings.Map(func(r rune) rune {
		upper := unicode.ToUpper(r)
		if r >= 128 && upper < 128 {
			return r
		}
		return upper
	}, s)
}

// Keep the token stream: decoding into a map loses repeated members and their order.
func jsonMemberCollisions(source string) []jsonMemberCollision {
	dec := json.NewDecoder(strings.NewReader(source))
	dec.UseNumber()
	var collisions []jsonMemberCollision
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 10000 {
			return errors.New("JSON nesting exceeds maximum depth")
		}
		token, err := dec.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]string{}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok {
					return errors.New("JSON object member is not a string")
				}
				folded := ordinalIgnoreCaseKey(key)
				if first, ok := seen[folded]; ok {
					collisions = append(collisions, jsonMemberCollision{first, key, dec.InputOffset()})
				} else {
					seen[folded] = key
				}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = dec.Token()
		case json.Delim('['):
			for dec.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = dec.Token()
		}
		return err
	}
	if walk(0) != nil {
		return nil // The JSON syntax check owns malformed input diagnostics.
	}
	return collisions
}
