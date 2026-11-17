package codec

import (
	"fmt"

	upstream "github.com/kjanat/go-yaml/v4"
)

var kinds = [...]struct {
	owned  Kind
	parser upstream.Kind
}{
	{0, 0},
	{DocumentNode, upstream.DocumentNode},
	{SequenceNode, upstream.SequenceNode},
	{MappingNode, upstream.MappingNode},
	{ScalarNode, upstream.ScalarNode},
	{AliasNode, upstream.AliasNode},
}

var styles = [...]struct {
	owned  Style
	parser upstream.Style
}{
	{TaggedStyle, upstream.TaggedStyle},
	{DoubleQuotedStyle, upstream.DoubleQuotedStyle},
	{SingleQuotedStyle, upstream.SingleQuotedStyle},
	{LiteralStyle, upstream.LiteralStyle},
	{FoldedStyle, upstream.FoldedStyle},
	{FlowStyle, upstream.FlowStyle},
}

func ownedKind(kind upstream.Kind) (Kind, error) {
	for _, pair := range kinds {
		if kind == pair.parser {
			return pair.owned, nil
		}
	}
	return 0, fmt.Errorf("unsupported parser YAML node kind %d", kind)
}

func parserKind(kind Kind) (upstream.Kind, error) {
	for _, pair := range kinds {
		if kind == pair.owned {
			return pair.parser, nil
		}
	}
	return 0, fmt.Errorf("unsupported YAML node kind %d", kind)
}

func ownedStyle(style upstream.Style) (Style, error) {
	var result Style
	for _, pair := range styles {
		if style&pair.parser != 0 {
			result |= pair.owned
			style &^= pair.parser
		}
	}
	if style != 0 {
		return 0, fmt.Errorf("unsupported parser YAML style bits %d", style)
	}
	return result, nil
}

func parserStyle(style Style) (upstream.Style, error) {
	var result upstream.Style
	for _, pair := range styles {
		if style&pair.owned != 0 {
			result |= pair.parser
			style &^= pair.owned
		}
	}
	if style != 0 {
		return 0, fmt.Errorf("unsupported YAML style bits %d", style)
	}
	return result, nil
}
