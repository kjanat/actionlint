package config

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

func MergeWarnings(groups ...[]Warning) []Warning {
	var result []Warning
	seen := map[Warning]bool{}
	for _, group := range groups {
		for _, warning := range group {
			if !seen[warning] {
				seen[warning] = true
				result = append(result, warning)
			}
		}
	}
	return result
}

// Only these legacy mappings permit unknown fields. New tool and policy settings
// already reject unknown keys in their decoders.
func configWarnings(root *yaml.Node, inputs map[*yaml.Node]Input) []Warning {
	var warnings []Warning
	var check func(*yaml.Node, string, []string)
	check = func(node *yaml.Node, prefix string, allowed []string) {
		if node == nil || node.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			name := prefix + key.Value
			if !slices.Contains(allowed, key.Value) {
				warnings = append(warnings, Warning{
					File: inputs[key].File, Line: key.Line, Column: key.Column,
					Message: fmt.Sprintf("unknown configuration key %q; ignored. Expected one of: %s", name, strings.Join(allowed, ", ")),
				})
				continue
			}
			if prefix != "" {
				continue
			}
			switch key.Value {
			case "self-hosted-runner":
				check(value, name+".", []string{"labels"})
			case "paths":
				if value.Kind == yaml.MappingNode {
					for j := 0; j < len(value.Content); j += 2 {
						check(value.Content[j+1], name+"["+value.Content[j].Value+"].", []string{"ignore"})
					}
				}
			}
		}
	}
	check(root, "", append(append(Keys(), legacyKeys...), "$schema"))
	return warnings
}
