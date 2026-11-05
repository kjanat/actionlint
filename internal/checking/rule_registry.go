package checking

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// CombineRuleDeclarations combines built-in and plugin declarations and rejects duplicate names.
func CombineRuleDeclarations(additional ...rule.Descriptor) ([]rule.Descriptor, error) {
	catalog := ListRules()
	names := map[string]bool{}
	for _, descriptor := range catalog {
		names[descriptor.Name] = true
	}
	for _, descriptor := range additional {
		if strings.TrimSpace(descriptor.Name) == "" {
			return nil, errors.New("rule name must not be empty")
		}
		if names[descriptor.Name] {
			return nil, fmt.Errorf("duplicate rule name %q", descriptor.Name)
		}
		names[descriptor.Name] = true
		catalog = append(catalog, descriptor.Clone())
	}
	slices.SortFunc(catalog, func(a, b rule.Descriptor) int { return strings.Compare(a.Name, b.Name) })
	return catalog, nil
}
