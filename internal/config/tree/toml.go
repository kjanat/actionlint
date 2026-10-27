package tree

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/kjanat/go-yaml/v4"
	"github.com/pelletier/go-toml/v2/unstable"
)

type tableOrigin uint8

const (
	tableImplicit tableOrigin = iota
	tableHeader
	tableDotted
	tableInline
	tableArray
)

type tomlDecoder struct {
	parser  unstable.Parser
	data    []byte
	origins map[*yaml.Node]tableOrigin
}

func decodeTOML(data []byte) (*yaml.Node, error) {
	d := &tomlDecoder{data: data, origins: map[*yaml.Node]tableOrigin{}}
	d.parser.Reset(data)
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: 1, Column: 1}
	current := root
	for d.parser.NextExpression() {
		expression := d.parser.Expression()
		var err error
		switch expression.Kind {
		case unstable.KeyValue:
			err = d.keyValue(current, expression)
		case unstable.Table:
			current, err = d.table(root, expression)
		case unstable.ArrayTable:
			current, err = d.arrayTable(root, expression)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := d.parser.Error(); err != nil {
		return nil, d.syntaxError(err)
	}
	return root, nil
}

func (d *tomlDecoder) syntaxError(err error) error {
	var parseErr *unstable.ParserError
	if !errors.As(err, &parseErr) {
		return err
	}
	offset := len(d.data)
	if len(parseErr.Highlight) > 0 {
		offset = int(d.parser.Range(parseErr.Highlight).Offset)
	}
	line, column := d.positionAt(offset)
	return fmt.Errorf("line %d, column %d: %s", line, column, parseErr.Message)
}

func (d *tomlDecoder) position(raw unstable.Range) (line, column int) {
	return d.positionAt(int(raw.Offset))
}

func (d *tomlDecoder) positionAt(offset int) (line, column int) {
	if offset > len(d.data) {
		offset = len(d.data)
	}
	prefix := d.data[:offset]
	line = 1 + bytes.Count(prefix, []byte{'\n'})
	column = 1 + utf8.RuneCount(prefix[bytes.LastIndexByte(prefix, '\n')+1:])
	return line, column
}

func (d *tomlDecoder) keyError(key *unstable.Node, format string, args ...any) error {
	line, column := d.position(key.Raw)
	return fmt.Errorf("line %d, column %d: %s", line, column, fmt.Sprintf(format, args...))
}

func keyParts(expression *unstable.Node) []*unstable.Node {
	var parts []*unstable.Node
	for it := expression.Key(); it.Next(); {
		parts = append(parts, it.Node())
	}
	return parts
}

func lookup(table *yaml.Node, name string) *yaml.Node {
	for i := 0; i+1 < len(table.Content); i += 2 {
		if table.Content[i].Value == name {
			return table.Content[i+1]
		}
	}
	return nil
}

func (d *tomlDecoder) keyNode(key *unstable.Node) *yaml.Node {
	line, column := d.position(key.Raw)
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(key.Data), Line: line, Column: column}
}

func (d *tomlDecoder) child(table *yaml.Node, key *unstable.Node, origin tableOrigin) (*yaml.Node, error) {
	name := string(key.Data)
	existing := lookup(table, name)
	if existing == nil {
		node := d.keyNode(key)
		node.Kind, node.Tag, node.Value = yaml.MappingNode, "!!map", ""
		d.origins[node] = origin
		table.Content = append(table.Content, d.keyNode(key), node)
		return node, nil
	}
	switch existing.Kind {
	case yaml.MappingNode:
		switch d.origins[existing] {
		case tableInline:
			return nil, d.keyError(key, "cannot extend inline table %q", name)
		case tableHeader:
			if origin == tableDotted {
				return nil, d.keyError(key, "cannot extend table %q with dotted keys", name)
			}
		}
		return existing, nil
	case yaml.SequenceNode:
		if origin == tableImplicit && d.origins[existing] == tableArray {
			return existing.Content[len(existing.Content)-1], nil
		}
		return nil, d.keyError(key, "cannot extend array %q", name)
	default:
		return nil, d.keyError(key, "key %q is not a table", name)
	}
}

func (d *tomlDecoder) keyValue(table *yaml.Node, expression *unstable.Node) error {
	parts := keyParts(expression)
	for _, part := range parts[:len(parts)-1] {
		var err error
		table, err = d.child(table, part, tableDotted)
		if err != nil {
			return err
		}
	}
	last := parts[len(parts)-1]
	name := string(last.Data)
	if lookup(table, name) != nil {
		return d.keyError(last, "key %q defined twice", name)
	}
	key := d.keyNode(last)
	value, err := d.value(expression.Value(), key.Line, key.Column)
	if err != nil {
		return err
	}
	table.Content = append(table.Content, key, value)
	return nil
}

func (d *tomlDecoder) table(root *yaml.Node, expression *unstable.Node) (*yaml.Node, error) {
	parts := keyParts(expression)
	table := root
	for _, part := range parts[:len(parts)-1] {
		var err error
		table, err = d.child(table, part, tableImplicit)
		if err != nil {
			return nil, err
		}
	}
	last := parts[len(parts)-1]
	name := string(last.Data)
	if existing := lookup(table, name); existing != nil {
		if existing.Kind == yaml.MappingNode && d.origins[existing] == tableImplicit {
			d.origins[existing] = tableHeader
			return existing, nil
		}
		return nil, d.keyError(last, "table %q defined twice", name)
	}
	return d.child(table, last, tableHeader)
}

func (d *tomlDecoder) arrayTable(root *yaml.Node, expression *unstable.Node) (*yaml.Node, error) {
	parts := keyParts(expression)
	table := root
	for _, part := range parts[:len(parts)-1] {
		var err error
		table, err = d.child(table, part, tableImplicit)
		if err != nil {
			return nil, err
		}
	}
	last := parts[len(parts)-1]
	name := string(last.Data)
	sequence := lookup(table, name)
	if sequence == nil {
		sequence = d.keyNode(last)
		sequence.Kind, sequence.Tag, sequence.Value = yaml.SequenceNode, "!!seq", ""
		d.origins[sequence] = tableArray
		table.Content = append(table.Content, d.keyNode(last), sequence)
	} else if sequence.Kind != yaml.SequenceNode || d.origins[sequence] != tableArray {
		return nil, d.keyError(last, "cannot append a table to %q", name)
	}
	element := d.keyNode(last)
	element.Kind, element.Tag, element.Value = yaml.MappingNode, "!!map", ""
	d.origins[element] = tableHeader
	sequence.Content = append(sequence.Content, element)
	return element, nil
}

func (d *tomlDecoder) value(node *unstable.Node, line, column int) (*yaml.Node, error) {
	if node.Raw.Length != 0 {
		line, column = d.position(node.Raw)
	}
	scalar := func(tag, value string, style yaml.Style) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value, Style: style, Line: line, Column: column}
	}
	switch node.Kind {
	case unstable.String:
		return scalar("!!str", string(node.Data), yaml.DoubleQuotedStyle), nil
	case unstable.Bool:
		return scalar("!!bool", string(node.Data), 0), nil
	case unstable.Integer:
		text := strings.ReplaceAll(string(node.Data), "_", "")
		number, ok := new(big.Int).SetString(text, 0)
		if !ok {
			return nil, fmt.Errorf("line %d, column %d: invalid integer %q", line, column, node.Data)
		}
		return scalar("!!int", number.String(), 0), nil
	case unstable.Float:
		text := strings.ReplaceAll(string(node.Data), "_", "")
		switch strings.TrimPrefix(text, "+") {
		case "inf":
			text = ".inf"
		case "-inf":
			text = "-.inf"
		case "nan", "-nan":
			text = ".nan"
		}
		return scalar("!!float", text, 0), nil
	case unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		return scalar("!!str", string(node.Data), yaml.DoubleQuotedStyle), nil
	case unstable.Array:
		sequence := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Line: line, Column: column}
		for it := node.Children(); it.Next(); {
			element, err := d.value(it.Node(), line, column)
			if err != nil {
				return nil, err
			}
			if len(sequence.Content) == 0 {
				sequence.Line, sequence.Column = element.Line, element.Column
			}
			sequence.Content = append(sequence.Content, element)
		}
		return sequence, nil
	case unstable.InlineTable:
		table := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: line, Column: column}
		d.origins[table] = tableInline
		for it := node.Children(); it.Next(); {
			if err := d.keyValue(table, it.Node()); err != nil {
				return nil, err
			}
		}
		return table, nil
	default:
		return nil, fmt.Errorf("line %d, column %d: unsupported value %s", line, column, node.Kind)
	}
}
