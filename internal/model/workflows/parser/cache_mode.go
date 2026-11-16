package parser

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func CacheModeFromYAML(n *codec.Node) *definition.CacheMode {
	if n == nil || n.Kind == 0 {
		return nil
	}
	m := &definition.CacheMode{Pos: PosAt(n)}
	if n.Kind == codec.ScalarNode && n.Tag == definition.YamlTagStr {
		value := n.Value
		if literal := syntax.LiteralExpressionValue(value); literal != nil {
			value = *literal
		}
		switch value {
		case "none":
			m.Kind = definition.CacheModeNone
		case "read":
			m.Kind = definition.CacheModeRead
		case "write":
			m.Kind = definition.CacheModeWrite
		case "write-only":
			m.Kind = definition.CacheModeWriteOnly
		}
	}
	return m
}

func (p *Parser) parseCacheMode(n *codec.Node) *definition.CacheMode {
	m := CacheModeFromYAML(n)
	if n.Kind != codec.ScalarNode || n.Tag != definition.YamlTagStr {
		p.typeErrorf(n, "expected string for \"cache-mode\" but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
	} else if m.Kind == definition.CacheModeInvalid {
		p.typeErrorf(n, "%q is invalid for \"cache-mode\". expected one of \"read\", \"write\", \"write-only\", \"none\"", n.Value)
	}
	return m
}
