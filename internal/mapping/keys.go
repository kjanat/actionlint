package mapping

import (
	"strings"

	"go.yaml.in/yaml/v4"
)

func (r Resolver[S]) InsertedKeys(node *yaml.Node, source func(*yaml.Node) S, insertion func(string) bool) map[string]S {
	keys := map[string]S{}
	var collect func(*yaml.Node)
	collect = func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode {
			text := source(n)
			if value, known := r.Evaluate(text); known {
				if object, ok := value.(map[string]any); ok {
					for name := range object {
						keys[strings.ToLower(name)] = r.NewText(name, text)
					}
				}
			}
			return
		}
		if n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i < len(n.Content); i += 2 {
			key := source(n.Content[i])
			if insertion(r.Text(key)) {
				collect(n.Content[i+1])
			} else if resolved, known := r.Key(key); known {
				keys[strings.ToLower(r.Text(resolved))] = resolved
			}
		}
	}
	collect(node)
	return keys
}
