package workspace

import (
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
)

func configurationSnapshot(report configreport.Report) configreport.Report {
	report.Overrides = slices.Clone(report.Overrides)
	report.FileDigests = maps.Clone(report.FileDigests)
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
