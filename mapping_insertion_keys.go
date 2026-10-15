package actionlint

import (
	"strings"

	"go.yaml.in/yaml/v4"
)

// Collect known inserted keys; unresolved expressions leave their names unknown.
func knownInsertedMappingKeys(node *yaml.Node) map[string]*String {
	keys := map[string]*String{}
	var collect func(*yaml.Node)
	collect = func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode {
			if value, known := workflowExpressionLiteral(newString(n)); known {
				if object, ok := value.(map[string]any); ok {
					for name := range object {
						keys[strings.ToLower(name)] = &String{Value: name, Pos: posAt(n)}
					}
				}
			}
			return
		}
		if n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i < len(n.Content); i += 2 {
			key := newString(n.Content[i])
			if isMappingInsertion(key.Value) {
				collect(n.Content[i+1])
			} else if !key.ContainsExpression() {
				keys[strings.ToLower(key.Value)] = key
			}
		}
	}
	collect(node)
	return keys
}
