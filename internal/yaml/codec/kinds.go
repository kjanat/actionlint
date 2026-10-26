package codec

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"

	upstream "go.yaml.in/yaml/v4"
)

var kinds = [...]struct {
	owned  syntax.Kind
	parser upstream.Kind
}{
	{0, 0},
	{syntax.DocumentNode, upstream.DocumentNode},
	{syntax.SequenceNode, upstream.SequenceNode},
	{syntax.MappingNode, upstream.MappingNode},
	{syntax.ScalarNode, upstream.ScalarNode},
	{syntax.AliasNode, upstream.AliasNode},
}

var styles = [...]struct {
	owned  syntax.Style
	parser upstream.Style
}{
	{syntax.TaggedStyle, upstream.TaggedStyle},
	{syntax.DoubleQuotedStyle, upstream.DoubleQuotedStyle},
	{syntax.SingleQuotedStyle, upstream.SingleQuotedStyle},
	{syntax.LiteralStyle, upstream.LiteralStyle},
	{syntax.FoldedStyle, upstream.FoldedStyle},
	{syntax.FlowStyle, upstream.FlowStyle},
}

func ownedKind(kind upstream.Kind) (syntax.Kind, error) {
	for _, pair := range kinds {
		if kind == pair.parser {
			return pair.owned, nil
		}
	}
	return 0, fmt.Errorf("unsupported parser YAML node kind %d", kind)
}

func parserKind(kind syntax.Kind) (upstream.Kind, error) {
	for _, pair := range kinds {
		if kind == pair.owned {
			return pair.parser, nil
		}
	}
	return 0, fmt.Errorf("unsupported YAML node kind %d", kind)
}

func ownedStyle(style upstream.Style) (syntax.Style, error) {
	var result syntax.Style
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

func parserStyle(style syntax.Style) (upstream.Style, error) {
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
