package actionlint

import (
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// Metadata diagnostics use the same verified scalar mappings as script analyzers.
// Unsupported YAML styles and folded expression strings retain the value position.
func metadataExpressionPosition(meta *ActionMetadata, value *ActionExprString, offset int) *Pos {
	fallback := &Pos{Line: value.Line, Col: value.Column}
	if offset <= 0 || offset > len(value.Value) {
		return fallback
	}
	var scalar *yaml.Node
	var find func(*yaml.Node)
	find = func(node *yaml.Node) {
		if node == nil || scalar != nil {
			return
		}
		if node.Kind == yaml.ScalarNode && node.Line == value.Line && node.Column == value.Column && node.Value == value.Value {
			scalar = node
			return
		}
		// Anchors already occur in Content. Following Alias would revisit them.
		for _, child := range node.Content {
			find(child)
		}
	}
	find(meta.schemaRoot)
	if scalar == nil {
		return fallback
	}
	parser := &parser{sourceLines: splitSourceLines(meta.src)}
	source := parser.scriptSource(scalar)
	prefix := value.Value[:offset-1]
	line := strings.Count(prefix, "\n") + 1
	column := utf8.RuneCountInString(prefix[strings.LastIndexByte(prefix, '\n')+1:]) + 1
	if pos, ok := source.pos(line, column); ok {
		return pos
	}
	return fallback
}
