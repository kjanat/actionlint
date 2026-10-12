package actionlint

import (
	"strings"
	"testing"
)

func TestMatrixExpandedInsertionChecks(t *testing.T) {
	for _, tc := range []struct{ name, matrix, want string }{
		{"axis insertion duplicate", `os: [ubuntu, "${{ fromJSON('[\"ubuntu\"]') }}"]`, "duplicate value"},
		{"insertion internal duplicate", `os: ["${{ fromJSON('[\"ubuntu\",\"ubuntu\"]') }}"]`, "duplicate value"},
		{"whole axis duplicate", `os: "${{ fromJSON('[\"ubuntu\",\"ubuntu\"]') }}"`, "duplicate value"},
		{"duplicate after unknown insertion", `os: ["${{ vars.AXIS }}", ubuntu, "${{ fromJSON('[\"ubuntu\"]') }}"]`, "duplicate value"},
		{"repeated dynamic source", `os: ["${{ vars.OS }}", "${{ vars.OS }}"]`, "duplicate value"},
		{"nested arrays retained", `os: [ubuntu, "${{ fromJSON('[[\"ubuntu\"]]') }}"]`, ""},
		{"inserted nested array duplicate", `os: [[ubuntu], "${{ fromJSON('[[\"ubuntu\"]]') }}"]`, "duplicate value"},
		{"distinct scalar types", `os: [1, "${{ fromJSON('[\"1\"]') }}"]`, ""},
		{"empty axis insertion", `os: [ubuntu, "${{ fromJSON('[]') }}"]`, ""},
		{"exclude insertion mismatch", `os: [ubuntu], exclude: ["${{ fromJSON('[{\"os\":\"missing\"}]') }}"]`, "does not match"},
		{"exclude insertion unknown axis", `os: [ubuntu], exclude: ["${{ fromJSON('[{\"arch\":\"arm\"}]') }}"]`, "does not exist"},
		{"exclude object insertion", `os: [ubuntu], exclude: ["${{ fromJSON('{\"os\":\"missing\"}') }}"]`, "does not match"},
		{"whole exclude expression", `os: [ubuntu], exclude: "${{ fromJSON('[{\"os\":\"missing\"}]') }}"`, "does not match"},
		{"matching axis insertion", `os: ["${{ fromJSON('[\"ubuntu\"]') }}"], exclude: [{os: ubuntu}]`, ""},
		{"nonmatching axis insertion", `os: ["${{ fromJSON('[\"ubuntu\"]') }}"], exclude: [{os: missing}]`, "does not match"},
		{"unknown axis insertion", `os: ["${{ vars.AXIS }}"], exclude: ["${{ fromJSON('[{\"os\":\"missing\"}]') }}"]`, ""},
		{"unknown exclude insertion", `os: [ubuntu], exclude: ["${{ vars.EXCLUDE }}"]`, ""},
		{"known exclude after unknown", `os: [ubuntu], exclude: ["${{ vars.EXCLUDE }}", "${{ fromJSON('[{\"os\":\"missing\"}]') }}"]`, "does not match"},
		{"invalid nested exclude insertion", `os: [ubuntu], exclude: ["${{ fromJSON('[[{\"os\":\"missing\"}]]') }}"]`, ""},
		{"returned exclude syntax stays data", `os: [ubuntu], exclude: ["${{ fromJSON('[{\"os\":\"${{ vars.OS }}\"}]') }}"]`, "does not match"},
		{"returned axis syntax stays data", `os: ["${{ fromJSON('[\"${{ vars.OS }}\"]') }}"], exclude: [{os: ubuntu}]`, "does not match"},
		{"returned nested axis syntax stays data", `os: ["${{ fromJSON('[{\"name\":\"${{ vars.OS }}\"}]') }}"], exclude: [{os: {name: ubuntu}}]`, "does not match"},
		{"unknown expression differs from returned data", `os: ["${{ vars.OS }}", "${{ fromJSON('[\"${{ vars.OS }}\"]') }}"]`, ""},
		{"unknown nested expression differs from returned data", `os: [{name: "${{ vars.OS }}"}, "${{ fromJSON('[{\"name\":\"${{ vars.OS }}\"}]') }}"]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix: {` + tc.matrix + `}
    steps:
      - run: echo ok
`
			workflow, findings := Parse([]byte(source))
			if len(findings) != 0 {
				t.Fatal(findings)
			}
			rule := NewRuleMatrix()
			if err := rule.VisitJobPre(workflow.Jobs["test"]); err != nil {
				t.Fatal(err)
			}
			findings = rule.Errs()
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected diagnostics: %v", findings)
				}
			} else if len(findings) != 1 || !strings.Contains(findings[0].Message, tc.want) || findings[0].Line != 6 || findings[0].Column < 16 {
				t.Fatalf("want one %q at matrix source, got %v", tc.want, findings)
			}
			var position *Pos
			switch tc.name {
			case "axis insertion duplicate":
				position = workflow.Jobs["test"].Strategy.Matrix.Rows["os"].Values[1].Pos()
			case "exclude insertion mismatch", "exclude insertion unknown axis":
				position = workflow.Jobs["test"].Strategy.Matrix.Exclude.Combinations[0].Expression.Pos
			}
			if position != nil && (findings[0].Line != position.Line || findings[0].Column != position.Col) {
				t.Fatalf("expanded finding position = %v, want %v", findings[0], position)
			}
		})
	}
}
