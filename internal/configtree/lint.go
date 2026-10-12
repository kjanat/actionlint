package configtree

import (
	"slices"

	"go.yaml.in/yaml/v4"
)

// SelectLintRules projects a lint tree without altering reset or preset semantics.
func SelectLintRules(node *yaml.Node, rules map[string][]string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return node
	}
	next := *node
	next.Content = nil
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		switch key.Value {
		case "enabled":
			next.Content = append(next.Content, key, value)
		case "rules":
			next.Content = append(next.Content, key, selectRuleNode(value, "", rules))
		}
	}
	return &next
}

func selectRuleNode(node *yaml.Node, category string, rules map[string][]string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return node
	}
	next := *node
	next.Content = nil
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		_, selectedCategory := rules[key.Value]
		switch {
		case key.Value == "preset", category != "" && key.Value == "level", category == "" && key.Value == "disable":
			next.Content = append(next.Content, key, value)
		case category == "" && selectedCategory:
			next.Content = append(next.Content, key, selectRuleNode(value, key.Value, rules))
		case category != "" && slices.Contains(rules[category], key.Value):
			next.Content = append(next.Content, key, value)
		}
	}
	return &next
}
