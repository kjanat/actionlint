package parser

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func resolveInsertedMapping(node *codec.Node) mappings.Mapping {
	return mappings.Resolve(mappingInputsFromYAML(node))
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

func mappingInputsFromYAML(node *codec.Node) *mappings.DeclaredMapping {
	if node == nil {
		return nil
	}
	if node.Kind == codec.ScalarNode {
		text := newString(node)
		if text != nil && text.ContainsExpression() {
			return &mappings.DeclaredMapping{Expression: definition.SemanticValue(text)}
		}
		return nil
	}
	if node.Kind != codec.MappingNode {
		return nil
	}
	result := &mappings.DeclaredMapping{Values: map[string]mappings.DeclaredEntry{}}
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
		result.Values[key.Value] = mappings.DeclaredEntry{Name: definition.SemanticValue(key), Source: key.Value}
	}
	return result
}

func knownMappingKey(key *definition.String) (*definition.String, bool) {
	name, state := mappings.ResolveKey(definition.SemanticValue(key))
	if state != mappings.KnownKey {
		return nil, false
	}
	result := *key
	result.Value = name
	return &result, true
}
