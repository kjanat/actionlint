package workflow_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestCatalogueWorkflowExamples(t *testing.T) {
	t.Parallel()
	for _, descriptor := range checking.ListRules() {
		for _, example := range descriptor.Examples {
			t.Run(descriptor.Name+"/"+example.Description, func(t *testing.T) {
				t.Parallel()
				cfg, err := checking.ConfigurationRules().Parse(fmt.Appendf(nil, "lint: {rules: {%s: {%s: on}}}", checking.RuleCategory(descriptor.Name), descriptor.Name))
				if err != nil {
					t.Fatal(err)
				}
				for _, sample := range []struct {
					content string
					before  bool
				}{{example.Before, true}, {example.After, false}} {
					result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "example.yml", Content: []byte(sample.content), Config: cfg}})
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
