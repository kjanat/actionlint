package parser

import (
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func resolveInsertedMapping(node *codec.Node) mapping.Mapping {
	return mapping.Resolve(mappingInputsFromYAML(node))
}

func emptyResolvedMappingKey(key value.Value) bool {
	if !key.Known() {
		return false
	}
	if key.Kind() == value.Null {
		return true
	}
	name, stringValue := key.Text()
	return stringValue && name == ""
}

func mappingInputsFromYAML(node *codec.Node) *mapping.Inputs {
	if node == nil {
		return nil
	}
	if node.Kind == codec.ScalarNode {
		text := newString(node)
		if text != nil && text.ContainsExpression() {
			return &mapping.Inputs{Expression: workflowlang.SemanticValue(text)}
		}
		return nil
	}
	if node.Kind != codec.MappingNode {
		return nil
	}
	result := &mapping.Inputs{Values: map[string]mapping.DeclaredEntry{}}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valueNode := node.Content[i], node.Content[i+1]
		key := newString(keyNode)
		if key == nil {
			continue
		}
		if strings.EqualFold(key.Value, "${{ insert }}") || strings.EqualFold(key.Value, "${{insert}}") {
			result.Insertions = append(result.Insertions, mappingInputsFromYAML(valueNode))
			continue
		}
		result.Values[key.Value] = mapping.DeclaredEntry{Name: workflowlang.SemanticValue(key), Source: key.Value}
	}
	return result
}

func knownMappingKey(key *workflowlang.String) (*workflowlang.String, bool) {
	name, state := mapping.ResolveKey(workflowlang.SemanticValue(key))
	if state != mapping.KnownKey {
		return nil, false
	}
	result := *key
	result.Value = name
	return &result, true
}
