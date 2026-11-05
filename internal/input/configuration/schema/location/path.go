// Package schemapath represents concrete document locations and their schema keys.
package schemapath

import (
	"strconv"
	"strings"
)

type segment struct {
	key     string
	index   int
	isIndex bool
	loose   bool
}

// Path is an immutable sequence of mapping keys and sequence indexes.
// Its zero value names the document root.
type Path struct{ segments []segment }

func (p Path) append(s segment) Path {
	segments := make([]segment, len(p.segments)+1)
	copy(segments, p.segments)
	segments[len(p.segments)] = s
	return Path{segments}
}

// Key appends a declared schema property.
func (p Path) Key(key string) Path { return p.append(segment{key: key}) }

// LooseKey adds a user-defined mapping key, represented by a schema wildcard.
func (p Path) LooseKey(key string) Path { return p.append(segment{key: key, loose: true}) }

// Index adds a sequence element, represented by a schema wildcard.
func (p Path) Index(index int) Path { return p.append(segment{index: index, isIndex: true}) }

// Schema renders the case-insensitive lookup key used by availability tables.
func (p Path) Schema() string {
	var b strings.Builder
	for i, s := range p.segments {
		if i > 0 {
			b.WriteByte('.')
		}
		if s.isIndex || s.loose {
			b.WriteByte('*')
		} else {
			b.WriteString(strings.ToLower(s.key))
		}
	}
	return b.String()
}

// Display renders a concrete path with quoted keys and terminal-safe escapes.
func (p Path) Display() string {
	var b strings.Builder
	for i, s := range p.segments {
		switch {
		case s.isIndex:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.index))
			b.WriteByte(']')
		case identifier(s.key):
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.key)
		default:
			b.WriteString("['")
			for _, r := range s.key {
				switch {
				case r == '\'':
					b.WriteString("''")
				case r == '\\' || !strconv.IsPrint(r):
					quoted := strconv.QuoteRune(r)
					b.WriteString(quoted[1 : len(quoted)-1])
				default:
					b.WriteRune(r)
				}
			}
			b.WriteString("']")
		}
	}
	return b.String()
}

func identifier(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		valid := r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && (r == '-' || r >= '0' && r <= '9')
		if !valid {
			return false
		}
	}
	return true
}
