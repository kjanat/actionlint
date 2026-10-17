package engine

import (
	"testing"

	"actionlint.kjanat.dev/internal/rulecatalog"
)

func TestCatalogueWorkflowExamples(t *testing.T) {
	for _, descriptor := range rulecatalog.Rules() {
		for _, example := range descriptor.Examples {
			t.Run(descriptor.Name+"/"+example.Description, func(t *testing.T) {
				for _, sample := range []struct {
					content string
					before  bool
				}{{example.Before, true}, {example.After, false}} {
					result, err := runTestAnalysis(t.Context(), Request{Sources: []SourceUnit{{Path: "example.yml", Content: []byte(sample.content)}}})
					if err != nil {
						t.Fatal(err)
					}
					matched := false
					for _, finding := range result.Diagnostics {
						matched = matched || finding.Rule == descriptor.Name
					}
					if sample.before && !matched || !sample.before && len(result.Diagnostics) != 0 {
						t.Fatalf("before=%v: unexpected findings: %+v", sample.before, result.Diagnostics)
					}
				}
			})
		}
	}
}
