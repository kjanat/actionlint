package analysis

import (
	"errors"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/rule"
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
