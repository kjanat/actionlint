package action

import (
	"strconv"
	"strings"
)

type schemaPathSegment struct {
	key     string
	index   int
	isIndex bool
	loose   bool
}

// schemaPath is an immutable sequence of mapping keys and sequence indexes.
// Its zero value names the document root.
type schemaPath struct{ segments []schemaPathSegment }

func (p schemaPath) append(s schemaPathSegment) schemaPath {
	segments := make([]schemaPathSegment, len(p.segments)+1)
	copy(segments, p.segments)
	segments[len(p.segments)] = s
	return schemaPath{segments}
}

// Key appends a declared schema property.
func (p schemaPath) Key(key string) schemaPath { return p.append(schemaPathSegment{key: key}) }

// LooseKey adds a user-defined mapping key, represented by a schema wildcard.
func (p schemaPath) LooseKey(key string) schemaPath {
	return p.append(schemaPathSegment{key: key, loose: true})
}

// Index adds a sequence element, represented by a schema wildcard.
func (p schemaPath) Index(index int) schemaPath {
	return p.append(schemaPathSegment{index: index, isIndex: true})
}

// Schema renders the case-insensitive lookup key used by availability tables.
func (p schemaPath) Schema() string {
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
func (p schemaPath) Display() string {
	var b strings.Builder
	for i, s := range p.segments {
		switch {
		case s.isIndex:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.index))
			b.WriteByte(']')
		case schemaPathIdentifier(s.key):
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

func schemaPathIdentifier(key string) bool {
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
