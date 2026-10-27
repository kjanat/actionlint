package builtin_test

import (
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	runnerapi "actionlint.kjanat.dev/v2/runner"
)

func TestRunnerMatrixIncludeExcludeParity(t *testing.T) {
	for _, tc := range []struct {
		name, matrix, runsOn string
		want                 int
	}{
		{"include only", `{"include":[{"os":"ubuntu-lates"}]}`, "", 1},
		{"axis and include", `{"os":["ubuntu-lates"],"include":[{"os":"windows-lates"}]}`, "", 2},
		{"exclude introduces no labels", `{"os":["ubuntu-latest"],"exclude":[{"os":"windows-lates"}]}`, "", 0},
		{"declarations checked before exclusions", `{"os":["ubuntu-lates"],"exclude":[{"os":"ubuntu-lates"}]}`, "", 1},
		{"excluded cross OS pairings", `{"os":["ubuntu-latest","windows-latest"],"label":["linux","windows"],"exclude":[{"os":"ubuntu-latest","label":"windows"},{"os":"windows-latest","label":"linux"}]}`, `['${{ matrix.os }}', '${{ matrix.label }}']`, 0},
		{"two executed conflicts remain distinct", `{"os":["windows-latest","macos-latest"]}`, `['${{ matrix.os }}', ubuntu-latest]`, 2},
		{"no executed pairing", `{"os":["ubuntu-latest"],"exclude":[{"os":"ubuntu-latest"}]}`, `[linux, windows]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runsOn := tc.runsOn
			if runsOn == "" {
				runsOn = "${{ matrix.os }}"
			}
			var messages []string
			for _, strategy := range []string{
				"strategy:\n      matrix: " + tc.matrix,
				"strategy: ${{ fromJSON('{\"matrix\":" + tc.matrix + "}') }}",
			} {
				source := `on: push
jobs:
  test:
    ` + strategy + `
    runs-on: ` + runsOn + `
    steps:
      - run: echo ok
`
				content := []byte(source)
				parsed := documentparse.Workflow("workflow.yml", content)
				if len(parsed.Findings) != 0 {
					t.Fatal(parsed.Findings)
				}
				snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
				snapshot, err := snapshots.Document("workflow.yml", content, parsed)
				if err != nil {
					t.Fatal(err)
				}
				workflow, present := snapshot.Workflow()
				if !present {
					t.Fatal("workflow source snapshot unavailable")
				}
				rule := builtin.NewRuleRunnerLabel(snapshot)
				if err := rule.VisitJobPre(workflow.Jobs["test"]); err != nil {
					t.Fatal(err)
				}
				errs := rule.Errs()
				knowledge, err := runnerlang.Prepare(t.Context(), snapshot, nil, runnerapi.Options{}, "scope")
				if err != nil {
					t.Fatal(err)
				}
				production := productionRunnerRule(t, snapshot, knowledge)
				if err := production.VisitJobPre(workflow.Jobs["test"]); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(production.Errs(), errs) {
					t.Fatalf("production knowledge and direct check diverged: production=%v direct=%v", production.Errs(), errs)
				}
				if len(errs) != tc.want {
					t.Fatalf("wanted %d runner-label diagnostics, got %v", tc.want, errs)
				}
				if messages == nil {
					messages = make([]string, len(errs))
					for i, err := range errs {
						messages[i], _, _ = strings.Cut(err.Message, " defined at ")
					}
				} else {
					for i, err := range errs {
						message, _, _ := strings.Cut(err.Message, " defined at ")
						if message != messages[i] {
							t.Fatalf("literal/expression mismatch: %q != %q", err.Message, messages[i])
						}
					}
				}
			}
		})
	}
}

func productionRunnerRule(t *testing.T, snapshot *documentparse.SourceSnapshot, knowledge *runnerlang.Knowledge) workflowvisit.Rule {
	t.Helper()
	for _, descriptor := range builtin.Descriptors() {
		if descriptor.Name == "runner-label" {
			return descriptor.Build(builtin.Context{SourceSnapshot: snapshot, Runners: knowledge})
		}
	}
	t.Fatal("production runner-label adapter unavailable")
	return nil
}
