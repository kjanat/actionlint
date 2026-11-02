package workflow_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestCatalogueWorkflowExamples(t *testing.T) {
	for _, descriptor := range rulecatalog.Rules() {
		for _, example := range descriptor.Examples {
			t.Run(descriptor.Name+"/"+example.Description, func(t *testing.T) {
				cfg, err := repoconfig.Parse(fmt.Appendf(nil, "lint: {rules: {%s: {%s: on}}}", rulecatalog.ConfigGroup(descriptor.Name), descriptor.Name))
				if err != nil {
					t.Fatal(err)
				}
				for _, sample := range []struct {
					content string
					before  bool
				}{{example.Before, true}, {example.After, false}} {
					result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "example.yml", Content: []byte(sample.content), Config: cfg}})
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
