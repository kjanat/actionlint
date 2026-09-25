package actionlint

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// ConfigWarning identifies an ignored setting without rejecting legacy config files.
type ConfigWarning struct {
	Message string `json:"message" yaml:"message"`
	Line    int    `json:"line" yaml:"line"`
	Column  int    `json:"column" yaml:"column"`
}

// Only these legacy mappings permit unknown fields. New tool and policy settings
// already reject unknown keys in their decoders.
func configWarnings(root *yaml.Node) []ConfigWarning {
	var warnings []ConfigWarning
	var check func(*yaml.Node, string, []string)
	check = func(node *yaml.Node, prefix string, allowed []string) {
		if node == nil || node.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			name := prefix + key.Value
			if !slices.Contains(allowed, key.Value) {
				warnings = append(warnings, ConfigWarning{fmt.Sprintf("unknown configuration key %q; ignored. Expected one of: %s", name, strings.Join(allowed, ", ")), key.Line, key.Column})
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
	check(root, "", ConfigKeys())
	return warnings
}
