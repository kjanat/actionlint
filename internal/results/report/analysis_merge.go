package report

import (
	"bytes"
	"fmt"
	"maps"
	"reflect"
	"slices"

	rule "actionlint.kjanat.dev/v2/internal/model/rules"
)

// Merge combines completed or partial analyses in order. It rejects conflicting
// captures and rule metadata and keeps each diagnostic aligned with its source.
func Merge(parts ...Analysis) (Analysis, error) {
	var combined Analysis
	for _, part := range parts {
		if len(part.Diagnostics) != len(part.Sources) {
			return Analysis{}, fmt.Errorf("analysis has %d diagnostics and %d source records", len(part.Diagnostics), len(part.Sources))
		}
		for path, captured := range part.CapturedSources {
			if previous, exists := combined.CapturedSources[path]; exists &&
				(previous.Revision != captured.Revision || previous.Coordinates != captured.Coordinates || previous.CoordinatesAvailable != captured.CoordinatesAvailable || !bytes.Equal(previous.Content, captured.Content)) {
				return Analysis{}, fmt.Errorf("source %q changed between repository analyses", path)
			}
		}
		for _, descriptor := range part.Rules {
			for _, previous := range combined.Rules {
				if previous.Name == descriptor.Name && !reflect.DeepEqual(previous, descriptor) {
					return Analysis{}, fmt.Errorf("repositories provide conflicting metadata for rule %q; check them separately", descriptor.Name)
				}
			}
			if !slices.ContainsFunc(combined.Rules, func(previous rule.Descriptor) bool { return previous.Name == descriptor.Name }) {
				combined.Rules = append(combined.Rules, descriptor)
			}
		}
		if len(part.CapturedSources) != 0 && combined.CapturedSources == nil {
			combined.CapturedSources = make(map[string]CapturedSource)
		}
		maps.Copy(combined.CapturedSources, part.CapturedSources)
		appendAnalysis(&combined, part)
	}
	return combined, nil
}
