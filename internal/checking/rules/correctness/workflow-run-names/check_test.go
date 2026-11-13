package workflownames_test

import (
	"strings"
	"testing"

	namecheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/workflow-run-names"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestChecksSuppliedInventoryWithoutLoadingSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, filters, message string
		complete               bool
	}{
		{"matching", "['Build*']", "", true},
		{"missing", "[Missing]", "no workflow name matches", true},
		{"case-sensitive", "['build docs']", "no workflow name matches", true},
		{"incomplete", "[Missing]", "", false},
		{"all excluded", "['Build*', '!Build docs']", "exclude every matching workflow", true},
		{"dynamic", `["${{ inputs.producer }}"]`, "", true},
		{"literal expression", `["${{ 'Build docs' }}"]`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text := "on:\n  workflow_run:\n    workflows: " + tc.filters + "\n    types: [completed]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
			parsed, errors := workflowparser.Parse([]byte(text))
			if len(errors) != 0 {
				t.Fatal(errors)
			}
			workflow := declaration.Publish(text, parsed, false, "").Bind("consumer").Workflow()
			inventory := names.NewInventory([]string{"Build docs"}, tc.complete)
			findings := namecheck.Check(inventory, namecheck.WorkflowRunFilters(workflow))
			if tc.message == "" {
				if len(findings) != 0 {
					t.Fatal(findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Rule != "workflow-run-names" || findings[0].Start.Line != 3 || !strings.Contains(findings[0].Message, tc.message) {
				t.Fatalf("finding lost its rule, source or filter meaning: %+v", findings)
			}
		})
	}
}
