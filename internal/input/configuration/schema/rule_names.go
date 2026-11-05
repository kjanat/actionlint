package schema

import (
	"fmt"
	"slices"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// RuleNameChoices projects declared names and aliases to individual JSON Schema
// values. Deprecation annotates only the alias that declares it.
func RuleNameChoices(descriptors []rule.Descriptor) []map[string]any {
	var choices []map[string]any
	for _, descriptor := range descriptors {
		choices = append(choices, object{"type": "string", "const": descriptor.Name, "description": descriptor.Description})
		for _, alias := range descriptor.Aliases {
			choice := object{"type": "string", "const": alias.Name, "description": fmt.Sprintf("Alias for %s.", descriptor.Name)}
			if alias.Deprecated {
				choice["deprecated"] = true
				choice["description"] = fmt.Sprintf("Deprecated alias. Use %s instead.", descriptor.Name)
			}
			choices = append(choices, choice)
		}
	}
	slices.SortFunc(choices, compareNameChoices)
	return choices
}

func compareNameChoices(a, b object) int {
	left, _ := a["const"].(string)
	right, _ := b["const"].(string)
	return strings.Compare(left, right)
}
