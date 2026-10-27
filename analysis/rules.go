package analysis

import (
	"errors"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/rule"
)

// Rules returns built-in and registered analyzer metadata in name order.
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
