package rules_test

import (
	"reflect"
	"strings"
	"testing"

	runnerinput "actionlint.kjanat.dev/v2/internal/input/runners"

	"actionlint.kjanat.dev/v2/internal/checking"
	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	ruleexec "actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRunnerMatrixIncludeExcludeParity(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
				parsed := parsing.Workflow(content)
				if len(parsed.SyntaxErrors) != 0 {
					t.Fatal(parsed.SyntaxErrors)
				}
				_, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
				snapshot, err := snapshots.Document("workflow.yml", content, parsed)
				if err != nil {
					t.Fatal(err)
				}
				_, present := snapshot.WorkflowView()
				if !present {
					t.Fatal("workflow source snapshot unavailable")
				}
				declaration := matrixJob(t, snapshot, "test")
				rejections := runnerlang.LabelRejections(declaration, runnerlang.KnownLabels(nil))
				var errs []results.Diagnostic
				for _, rejection := range rejections {
					errs = append(errs, results.Diagnostic{Rule: "runner-label", Start: rejection.Position, Message: rejection.Reason})
				}
				knowledge, err := runnerinput.Prepare(t.Context(), snapshot, nil, runnerapi.Options{}, "scope")
				if err != nil {
					t.Fatal(err)
				}
				if tc.name == "excluded cross OS pairings" {
					declaration := matrixJob(t, snapshot, "test")
					answers := knowledge.Job(declaration).Answers
					operatingSystems := map[runnerapi.OS]int{}
					for _, answer := range answers {
						switch resolution := answer.Resolution.(type) {
						case runnerapi.Resolved:
							if len(resolution.Candidates) != 1 {
								t.Fatalf("pair did not resolve exactly one runner: %+v", resolution)
							}
							operatingSystems[resolution.Candidates[0].OS]++
						default:
							t.Fatalf("excluded-pair test did not execute a known valid pairing: %+v", answer)
						}
					}
					if len(answers) != 2 || operatingSystems[runnerapi.Linux] != 1 || operatingSystems[runnerapi.Windows] != 1 {
						t.Fatalf("excluded-pair scenario lost surviving execution combinations: %+v", answers)
					}
				}
				for _, production := range []workflowvisit.Rule{
					productionRunnerRule(t, snapshot, knowledge),
					declarationchecks.New(checking.ConfigurationRules(), correctness.RunnerLabelCheck(nil, runnerlang.KnownLabels(nil))),
				} {
					if err := production.VisitJobPre(matrixJob(t, snapshot, "test")); err != nil {
						t.Fatal(err)
					}
					findings := production.Errs()
					var got []results.Diagnostic
					if len(findings) != 0 {
						got = make([]results.Diagnostic, len(findings))
					}
					for i, finding := range findings {
						got[i] = finding.Diagnostic
					}
					if !reflect.DeepEqual(got, errs) {
						t.Fatalf("production knowledge and direct check diverged: production=%v direct=%v", got, errs)
					}
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

func productionRunnerRule(t *testing.T, snapshot parsing.SourceSnapshot, knowledge *runnerlang.Knowledge) workflowvisit.Rule {
	t.Helper()
	rules := checking.ConfigurationRules()
	for _, descriptor := range ruleexec.Registrations(rules) {
		if descriptor.Name == "runner-label" {
			return descriptor.Build(ruleexec.Inputs{Rules: rules, SourceSnapshot: snapshot, Runners: knowledge})
		}
	}
	t.Fatal("production runner-label adapter unavailable")
	return nil
}
