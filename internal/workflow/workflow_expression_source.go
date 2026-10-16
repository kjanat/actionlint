package workflow

import (
	"strings"

	"actionlint.kjanat.dev/internal/yaml"
)

func (p *Parser) workflowExpressionSources(root *yaml.Node) map[Pos]*ScriptSource {
	sources := make(map[Pos]*ScriptSource)
	seen := make(map[*yaml.Node]bool)
	var visit func(*yaml.Node, bool)
	visit = func(node *yaml.Node, condition bool) {
		if node == nil || seen[node] && node.Kind != yaml.ScalarNode {
			return
		}
		seen[node] = true
		if node.Kind == yaml.ScalarNode && (condition || strings.Contains(node.Value, "${{")) {
			if source := p.ScriptSource(node); source != nil {
				sources[*PosAt(node)] = source
			}
		}
		if node.Kind == yaml.MappingNode {
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
