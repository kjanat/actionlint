package parser

import (
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func CacheModeFromYAML(n *syntax.Node) *workflowlang.CacheMode {
	if n == nil || n.Kind == 0 {
		return nil
	}
	m := &workflowlang.CacheMode{Pos: PosAt(n)}
	if n.Kind == syntax.ScalarNode && n.Tag == workflowlang.YamlTagStr {
		value := n.Value
		if literal := exprlang.LiteralExpressionValue(value); literal != nil {
			value = *literal
		}
		switch value {
		case "none":
			m.Kind = workflowlang.CacheModeNone
		case "read":
			m.Kind = workflowlang.CacheModeRead
		case "write":
			m.Kind = workflowlang.CacheModeWrite
		case "write-only":
			m.Kind = workflowlang.CacheModeWriteOnly
		}
	}
	return m
}

func (p *Parser) parseCacheMode(n *syntax.Node) *workflowlang.CacheMode {
	m := CacheModeFromYAML(n)
	if n.Kind != syntax.ScalarNode || n.Tag != workflowlang.YamlTagStr {
		p.typeErrorf(n, "expected string for \"cache-mode\" but found %s node with %q tag", NodeKindName(n.Kind), n.Tag)
	} else if m.Kind == workflowlang.CacheModeInvalid {
		p.typeErrorf(n, "%q is invalid for \"cache-mode\". expected one of \"read\", \"write\", \"write-only\", \"none\"", n.Value)
	}
	return m
}
