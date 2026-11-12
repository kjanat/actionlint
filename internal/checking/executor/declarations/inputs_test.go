package declarations

import (
	"slices"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestAdapterUsesDeclaredInputsWithoutRuleNameDispatch(t *testing.T) {
	t.Parallel()
	const source = "name: Native\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: work\n        run: echo ok\n"
	parsed, errs := workflowparser.Parse([]byte(source))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	workflow := declaration.Publish(source, parsed, false, "").Bind("test").Workflow()
	job, found := workflow.Job("build")
	if !found {
		t.Fatal("fixture job is missing")
	}
	step := slices.Collect(job.Steps())[0]
	for _, scope := range []string{"workflow", "job", "step"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			check := checkinputs.DeclarationCheck{Name: "native-" + scope, Category: "correctness"}
			calls := 0
			switch scope {
			case "workflow":
				check.Workflow = func(input declaration.WorkflowView) []reporting.Finding {
					if input.Name().Value() != "Native" {
						t.Fatal("callback received a different workflow")
					}
					calls++
					return nil
				}
			case "job":
				check.Job = func(input declaration.JobView) []reporting.Finding {
					if input.ID().Value() != "build" {
						t.Fatal("callback received a different job")
					}
					calls++
					return nil
				}
			case "step":
				check.Step = func(input declaration.StepView) []reporting.Finding {
					if input.Reference() != step.Reference() {
						t.Fatal("callback received a different step")
					}
					calls++
					collector := reporting.Collector{Rule: check.Name, Step: input}
					collector.Errorf(reporting.SourcePosition(input), "test finding")
					return collector.Findings
				}
			}
			rules, err := repoconfig.NewRuleSet([]rule.Declaration{check.Declaration})
			if err != nil {
				t.Fatal(err)
			}
			adapter := New(rules, check)
			for _, err := range []error{adapter.VisitWorkflowPre(workflow), adapter.VisitJobPre(job), adapter.VisitStep(step)} {
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatalf("%s callback ran %d times", scope, calls)
			}
			if scope == "step" {
				findings := adapter.Errs()
				if len(findings) != 1 || findings[0].Owner == nil || findings[0].Owner.Job != "build" || findings[0].IgnoreOrigin == nil || findings[0].IgnoreOrigin.Job != "build" {
					t.Fatalf("step finding lost its declaring job: %+v", findings)
				}
			}
		})
	}
}
