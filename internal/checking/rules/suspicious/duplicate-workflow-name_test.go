package suspicious

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestDuplicateWorkflowNameUsesSuppliedInventory(t *testing.T) {
	t.Parallel()
	others := names.NewInventory(nil, false).WithLiteralNames([]names.NamedWorkflow{
		{Path: "/repo/.github/workflows/release-go.yml", Name: "Publish packages"},
		{Path: "/repo/.github/workflows/publish-crates.yml", Name: "publish packages"},
		{Path: "/repo/.github/workflows/bench.yml", Name: "Bench"},
	})
	for _, tc := range []struct {
		desc, source, message string
	}{
		{"duplicate", "name: Publish packages\n", `workflow name "Publish packages" is also used by .github/workflows/publish-crates.yml (as "publish packages"), .github/workflows/release-go.yml; the Actions tab cannot tell these workflows apart`},
		{"distinct", "name: Release\n", ""},
		{"unnamed", "", ""},
		{"expression", "name: ${{ 'Bench' }}\n", ""},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			text := tc.source + "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
			parsed, errs := parser.ParseWorkflow([]byte(text))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			workflow := workflows.Bind("checked", text, parsed, false, "").Workflow()
			findings := DuplicateWorkflowName(others, workflow)
			if tc.message == "" {
				if len(findings) != 0 {
					t.Fatal(findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Message != tc.message || findings[0].Rule != "duplicate-workflow-name" ||
				findings[0].Severity != "warning" || findings[0].Start.Line != 1 || findings[0].Start.Column != 7 {
				t.Fatalf("%+v", findings)
			}
		})
	}
}
