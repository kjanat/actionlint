package workflow

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

func (p *Parser) workflowExpressionSources(root *syntax.Node) map[Pos]*ScriptSource {
	sources := make(map[Pos]*ScriptSource)
	seen := make(map[*syntax.Node]bool)
	var visit func(*syntax.Node, bool)
	visit = func(node *syntax.Node, condition bool) {
		if node == nil || seen[node] && node.Kind != syntax.ScalarNode {
			return
		}
		seen[node] = true
		if node.Kind == syntax.ScalarNode && (condition || strings.Contains(node.Value, "${{")) {
			if source := p.ScriptSource(node); source != nil {
				sources[*PosAt(node)] = source
			}
		}
		if node.Kind == syntax.MappingNode {
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
