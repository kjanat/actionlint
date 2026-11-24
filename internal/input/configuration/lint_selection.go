package configuration

import (
	"maps"
	"slices"
)

func selectLintRules(value any, rules map[string][]string) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	next := maps.Clone(object)
	if child, present := object["rules"]; present {
		next["rules"] = selectRuleNode(child, "", rules)
	}
	return next
}

func selectRuleNode(value any, category string, rules map[string][]string) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	next := make(map[string]any)
	for key, child := range object {
		_, selected := rules[key]
		switch {
		case key == "preset", category != "" && key == "level", category == "" && key == "disable":
			next[key] = child
		case category == "" && selected:
			next[key] = selectRuleNode(child, key, rules)
		case category != "" && slices.Contains(rules[category], key):
			next[key] = child
		}
	}
	return next
}
