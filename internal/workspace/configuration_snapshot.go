package workspace

import (
	"maps"
	"slices"

	configreport "actionlint.kjanat.dev/v2/config/report"
)

// Configuration reports leave both the resolver and the session cache. Each
// publication owns its maps and slices, including the decoded effective values.
func configurationSnapshot(report configreport.Report) configreport.Report {
	report.Overrides = slices.Clone(report.Overrides)
	report.Inspection.Config = configurationMapSnapshot(report.Inspection.Config)
	report.Inspection.Origins = maps.Clone(report.Inspection.Origins)
	report.Inspection.Warnings = slices.Clone(report.Inspection.Warnings)
	return report
}

func configurationMapSnapshot(values map[string]any) map[string]any {
	result := maps.Clone(values)
	for key, value := range result {
		result[key] = configurationValueSnapshot(value)
	}
	return result
}

// EffectiveValues decodes a YAML value tree: mappings, sequences and immutable
// scalars. Copy its concrete container shapes without converting scalar types.
func configurationValueSnapshot(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return configurationMapSnapshot(value)
	case map[any]any:
		result := maps.Clone(value)
		for key, entry := range result {
			result[key] = configurationValueSnapshot(entry)
		}
		return result
	case []any:
		result := slices.Clone(value)
		for i, entry := range result {
			result[i] = configurationValueSnapshot(entry)
		}
		return result
	default:
		return value
	}
}
