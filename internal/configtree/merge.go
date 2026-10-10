// Package configtree owns immutable YAML alias expansion and configuration merging.
package configtree

import (
	"fmt"

	"go.yaml.in/yaml/v4"
)

// Input tracks explicit overlays and null resets during merging.
type Input struct {
	Name  string
	Reset *yaml.Node
	File  string
}

// Expand resolves aliases and YAML merge keys before overlaying. Otherwise replacing an
// anchor's value can invalidate aliases elsewhere in an unchanged section.
func Expand(node *yaml.Node, visiting map[*yaml.Node]bool) (*yaml.Node, error) {
	if node == nil {
		return nil, nil
	}
	if visiting[node] {
		return nil, fmt.Errorf("cyclic YAML alias at line %d", node.Line)
	}
	visiting[node] = true
	defer delete(visiting, node)
	if node.Kind == yaml.AliasNode {
		return Expand(node.Alias, visiting)
	}
	expandedNode := *node
	expandedNode.Anchor = ""
	expandedNode.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		expanded, err := Expand(child, visiting)
		if err != nil {
			return nil, err
		}
		expandedNode.Content[i] = expanded
	}
	if expandedNode.Kind != yaml.MappingNode {
		return &expandedNode, nil
	}
	entries := make([]*yaml.Node, 0, len(expandedNode.Content))
	keys := make(map[string]bool)
	var inherited []*yaml.Node
	for i := 0; i < len(expandedNode.Content); i += 2 {
		key, value := expandedNode.Content[i], expandedNode.Content[i+1]
		if key.ShortTag() != "!!merge" {
			entries = append(entries, key, value)
			keys[key.Value] = true
			continue
		}
		if value.Kind == yaml.SequenceNode {
			inherited = append(inherited, value.Content...)
		} else {
			inherited = append(inherited, value)
		}
	}
	// Explicit keys win; in a merge sequence, the first mapping wins.
	for _, mapping := range inherited {
		if mapping.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("YAML merge must contain mappings at line %d", mapping.Line)
		}
		for i := 0; i < len(mapping.Content); i += 2 {
			key, value := mapping.Content[i], mapping.Content[i+1]
			if !keys[key.Value] {
				entries = append(entries, key, value)
				keys[key.Value] = true
			}
		}
	}
	expandedNode.Content = entries
	return &expandedNode, nil
}

// Merge returns a fresh mapping, never mutating a loaded config or
// an input. Lists/scalars replace; explicit null resets; empty maps inherit.
func Merge(base, overlay *yaml.Node, inputs map[*yaml.Node]Input) *yaml.Node {
	for base != nil && base.Kind == yaml.AliasNode {
		base = base.Alias
	}
	for overlay.Kind == yaml.AliasNode {
		overlay = overlay.Alias
	}
	if base == nil || base.Kind != yaml.MappingNode || overlay.Kind != yaml.MappingNode {
		if base != nil && base.Tag == "!!null" && inputs[base].Name != "" && overlay.Kind == yaml.MappingNode {
			// A later partial mapping must retain the reset of its omitted fields.
			replacement := *overlay
			inputs[&replacement] = Input{Name: inputs[overlay].Name, Reset: base}
			return &replacement
		}
		return overlay
	}
	merged := *base
	inputs[&merged] = inputs[base]
	merged.Content = append([]*yaml.Node{}, base.Content...)
	for i := 0; i < len(overlay.Content); i += 2 {
		key, value := overlay.Content[i], overlay.Content[i+1]
		found := false
		for j := 0; j < len(merged.Content); j += 2 {
			if merged.Content[j].Value == key.Value {
				merged.Content[j+1] = Merge(merged.Content[j+1], value, inputs)
				found = true
				break
			}
		}
		if !found {
			merged.Content = append(merged.Content, key, value)
		}
	}
	return &merged
}
