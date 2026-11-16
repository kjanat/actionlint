package parser

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

// Check the physical tree before alias projection. YAML accepts compact empty
// flow values, but GitHub's reader can absorb the colon into an unquoted key.
// Quoted keys use a different token boundary and do not need this restriction.
func (p *Parser) checkFlowMappingNulls(node *codec.Node, flow bool) {
	flow = flow || node.Style&codec.FlowStyle != 0
	if flow && node.Kind == codec.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if p.compactFlowNull(key, value) {
				p.error(key, CodeFlowNull, "GitHub Actions requires a space after ':' before an empty flow-mapping value; insert a space or write an explicit null")
			}
		}
	}
	for _, child := range node.Content {
		p.checkFlowMappingNulls(child, flow)
	}
}

func (p *Parser) compactFlowNull(key, value *codec.Node) bool {
	if key.Kind != codec.ScalarNode || key.Value == "" || key.Style&(codec.SingleQuotedStyle|codec.DoubleQuotedStyle) != 0 ||
		value.Kind != codec.ScalarNode || value.Tag != definition.YamlTagNull || value.Value != "" {
		return false
	}
	row := value.Line - 1
	offset, ok := p.scalarDocument.ByteOffset(row, value.Column)
	if !ok {
		return false
	}
	line := p.sourceLines[row]
	if offset >= len(line) {
		return false
	}
	// Implicit pairs inside flow sequences point at the colon; ordinary flow
	// mappings point at the delimiter after it.
	colon := offset - 1
	if line[offset] == ':' {
		colon = offset
	}
	if colon < 0 || colon+1 >= len(line) || line[colon] != ':' || !strings.ContainsRune(",}]", rune(line[colon+1])) {
		return false
	}
	source := p.ScriptSource(key)
	if source == nil {
		return false
	}
	// An explicit key may have a comment before its value indicator. That
	// terminates the plain scalar, making even a compact separator unambiguous.
	for r := source.ScalarEnd.Line - 1; r <= row; r++ {
		start, end := 0, len(p.sourceLines[r])
		if r == source.ScalarEnd.Line-1 {
			start, ok = p.scalarDocument.ByteOffset(r, source.ScalarEnd.Column)
			if !ok {
				return false
			}
		}
		if r == row {
			end = colon
		}
		if start > end || strings.Trim(p.sourceLines[r][start:end], " \t") != "" {
			return false
		}
	}
	return true
}
