package document

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

func parseTOML(path string, data []byte) (Document, error) {
	d := newDocument(path)
	var value map[string]any
	if err := toml.Unmarshal(data, &value); err != nil {
		if decode, ok := errors.AsType[*toml.DecodeError](err); ok {
			line, column := decode.Position()
			return d, fmt.Errorf("%s:%d:%d: %w", path, line, column, err)
		}
		return d, err
	}
	if value == nil {
		value = map[string]any{}
	}
	normalized, err := normalize(value)
	if err != nil {
		return d, err
	}
	d.Value = normalized
	var parser unstable.Parser
	parser.Reset(data)
	current := ""
	arrays := map[string]int{}
	loc := func(n *unstable.Node) Location { return location(path, data, int(n.Raw.Offset)) }
	var indexValue func(*unstable.Node, string, int)
	keyPath := func(base string, n *unstable.Node, resolveArrays bool) string {
		it := n.Key()
		p := base
		for it.Next() {
			key := it.Node()
			p = Path(p, string(key.Data))
			if _, ok := d.Locations[p]; !ok {
				d.Locations[p] = loc(key)
			}
			if resolveArrays {
				if i, ok := arrays[p]; ok {
					p = Index(p, i)
				}
			}
		}
		return p
	}
	valueStart := func(n *unstable.Node) int {
		it := n.Key()
		offset := 0
		for it.Next() {
			k := it.Node()
			offset = int(k.Raw.Offset + k.Raw.Length)
		}
		for offset < len(data) && data[offset] != '=' {
			offset++
		}
		if offset < len(data) {
			offset++
		}
		for offset < len(data) && (data[offset] == ' ' || data[offset] == '\t') {
			offset++
		}
		return offset
	}
	indexValue = func(n *unstable.Node, p string, fallback int) {
		if n == nil {
			return
		}
		if n.Raw.Length > 0 {
			fallback = int(n.Raw.Offset)
		}
		d.ValueLocations[p] = location(path, data, fallback)
		if n.Kind == unstable.LocalDate || n.Kind == unstable.LocalTime || n.Kind == unstable.LocalDateTime || n.Kind == unstable.DateTime {
			setPath(d.Value, p, string(n.Data))
		}
		switch n.Kind {
		case unstable.Array:
			it := n.Children()
			i := 0
			cursor := fallback + 1
			for it.Next() {
				cursor = tomlNextValue(data, cursor)
				child := Index(p, i)
				start := cursor
				if it.Node().Raw.Length > 0 {
					start = int(it.Node().Raw.Offset)
				}
				d.Locations[child] = location(path, data, start)
				indexValue(it.Node(), child, start)
				cursor = tomlValueEnd(data, start)
				i++
			}
		case unstable.Invalid, unstable.Comment, unstable.Key, unstable.Table, unstable.ArrayTable, unstable.KeyValue,
			unstable.String, unstable.Bool, unstable.Float, unstable.Integer, unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		case unstable.InlineTable:
			it := n.Children()
			for it.Next() {
				entry := it.Node()
				child := keyPath(p, entry, false)
				indexValue(entry.Value(), child, valueStart(entry))
			}
		}
	}
	for parser.NextExpression() {
		n := parser.Expression()
		switch n.Kind {
		case unstable.Table:
			current = keyPath("", n, true)
			pos := 0
			it := n.Key()
			if it.Next() {
				pos = bytes.LastIndexByte(data[:int(it.Node().Raw.Offset)], '[')
				if pos < 0 {
					pos = int(it.Node().Raw.Offset)
				}
			}
			d.ValueLocations[current] = location(path, data, pos)
		case unstable.ArrayTable:
			parts := n.Key()
			p := ""
			var last *unstable.Node
			for parts.Next() {
				part := parts.Node()
				if last != nil {
					parent := Path(p, string(last.Data))
					if i, ok := arrays[parent]; ok {
						parent = Index(parent, i)
					}
					p = parent
				}
				last = part
			}
			if last != nil {
				p = Path(p, string(last.Data))
				d.Locations[p] = loc(last)
			}
			i := -1
			if prev, ok := arrays[p]; ok {
				i = prev
			}
			i++
			arrays[p] = i
			current = Index(p, i)
			pos := 0
			it := n.Key()
			if it.Next() {
				pos = bytes.LastIndexByte(data[:int(it.Node().Raw.Offset)], '[')
				if pos > 0 && data[pos-1] == '[' {
					pos--
				}
				if pos < 0 {
					pos = int(it.Node().Raw.Offset)
				}
			}
			d.Locations[current] = location(path, data, pos)
			d.ValueLocations[current] = location(path, data, pos)
		case unstable.Invalid, unstable.Comment, unstable.Key, unstable.Array, unstable.InlineTable,
			unstable.String, unstable.Bool, unstable.Float, unstable.Integer, unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		case unstable.KeyValue:
			p := keyPath(current, n, false)
			indexValue(n.Value(), p, valueStart(n))
		}
	}
	if err := parser.Error(); err != nil {
		return d, err
	}
	return d, nil
}

func tomlNextValue(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n', ',':
			i++
		case '#':
			for i < len(data) && data[i] != '\n' {
				i++
			}
		default:
			return i
		}
	}
	return i
}

func tomlValueEnd(data []byte, start int) int {
	depth := 0
	var quote byte
	triple := false
	escaped := false
	for i := start; i < len(data); i++ {
		c := data[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				if triple {
					if i+2 < len(data) && data[i+1] == quote && data[i+2] == quote {
						i += 2
						quote = 0
					}
				} else {
					quote = 0
				}
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			if i+2 < len(data) && data[i+1] == c && data[i+2] == c {
				triple = true
				i += 2
			} else {
				triple = false
			}
		case '#':
			for i < len(data) && data[i] != '\n' {
				i++
			}
		case '[', '{':
			depth++
		case ']', '}':
			if depth == 0 {
				return i
			}
			depth--
			if depth == 0 {
				return i + 1
			}
		case ',':
			if depth == 0 {
				return i
			}
		case '\n':
			if depth == 0 {
				return i
			}
		}
	}
	return len(data)
}
