package analysis

import (
	"errors"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/rulecatalog"
	"actionlint.kjanat.dev/rule"
)

// Rules returns built-in and registered analyzer metadata in name order. Duplicate
// IDs are rejected so configuration and diagnostics have one meaning per name.
func Rules(extensions ...analyzer.Analyzer) ([]rule.Descriptor, error) {
	descriptors := make([]rule.Descriptor, 0, len(extensions))
	for _, extension := range extensions {
		if extension == nil {
			return nil, errors.New("nil analyzer registration")
		}
		descriptors = append(descriptors, extension.Descriptor().Descriptor)
	}
	return rulecatalog.Combine(descriptors...)
}
