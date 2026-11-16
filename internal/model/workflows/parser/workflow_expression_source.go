package parser

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

// ExpressionSources maps expressions and conditions in a document to their
// physical scalar positions, including composite action metadata.
func (p *Parser) ExpressionSources(root *codec.Node) map[source.Position]*workflowlang.ScriptSource {
	sources := make(map[source.Position]*workflowlang.ScriptSource)
	seen := make(map[*codec.Node]bool)
	var visit func(*codec.Node, bool)
	visit = func(node *codec.Node, condition bool) {
		if node == nil || seen[node] && node.Kind != codec.ScalarNode {
			return
		}
		seen[node] = true
		if node.Kind == codec.ScalarNode && (condition || strings.Contains(node.Value, "${{")) {
			if source := p.ScriptSource(node); source != nil {
				sources[*PosAt(node)] = source
			}
		}
		if node.Kind == codec.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				visit(node.Content[i], false)
				visit(node.Content[i+1], strings.EqualFold(node.Content[i].Value, "if"))
			}
			return
		}
		for _, child := range node.Content {
			visit(child, false)
		}
	}
	visit(root, false)
	return sources
}
