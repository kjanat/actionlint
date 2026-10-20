package rulecatalog

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"actionlint.kjanat.dev/rule"
)

// Combine gives built-ins and extensions one collision-checked namespace.
func Combine(additional ...rule.Descriptor) ([]rule.Descriptor, error) {
	catalog := Rules()
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
