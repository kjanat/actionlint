package define

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestActionSupportComesFromDeclaration(t *testing.T) {
	t.Parallel()
	for _, supportsActions := range []bool{false, true} {
		t.Run(map[bool]string{false: "workflow", true: "workflow and actions"}[supportsActions], func(t *testing.T) {
			t.Parallel()
			native := rule.Declaration{Name: "new-native-check", Category: "correctness", SupportsActions: supportsActions}
			checks := Checks{Step: func(workflows.Step) []reporting.StepFinding { return nil }}
			rules, err := configuration.NewRuleSet([]rule.Declaration{native})
			if err != nil {
				t.Fatal(err)
			}
			_, participates := NewChecksVisitor(rules, native.Name, checks).(workflowvisit.InvocationParticipant)
			if participates != supportsActions {
				t.Fatalf("check declares action support %t, but composite participation is %t", supportsActions, participates)
			}
		})
	}
}

func TestAdapterUsesDeclaredInputsWithoutRuleNameDispatch(t *testing.T) {
	t.Parallel()
	const source = "name: Native\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: work\n        run: echo ok\n"
	parsed, errs := parser.ParseWorkflow([]byte(source))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	workflow := workflows.Bind("test", source, parsed, false, "").Workflow()
	job, found := workflow.Job("build")
	if !found {
		t.Fatal("fixture job is missing")
	}
	step := slices.Collect(job.Steps())[0]
	for _, scope := range []string{"workflow", "job", "step"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			native := rule.Declaration{Name: "native-" + scope, Category: "correctness"}
			var checks Checks
			calls := 0
			switch scope {
			case "workflow":
				checks.Workflow = func(input workflows.Workflow) []reporting.StepFinding {
					if input.Name.Text() != "Native" {
						t.Fatal("callback received a different workflow")
					}
					calls++
					return nil
				}
			case "job":
				checks.Job = func(input workflows.Job) []reporting.StepFinding {
					if input.ID.Text() != "build" {
						t.Fatal("callback received a different job")
					}
					calls++
					return nil
				}
			case "step":
				checks.Step = func(input workflows.Step) []reporting.StepFinding {
					if input.Reference() != step.Reference() {
						t.Fatal("callback received a different step")
					}
					calls++
					collector := reporting.Collector{Step: input}
					collector.Errorf(sourcetext.NilUnlessPresent(input.Position()), "test finding")
					return collector.Findings
				}
			}
			rules, err := configuration.NewRuleSet([]rule.Declaration{native})
			if err != nil {
				t.Fatal(err)
			}
			adapter := NewChecksVisitor(rules, native.Name, checks)
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
				if len(findings) != 1 || findings[0].Rule != native.Name || findings[0].Owner == nil || findings[0].Owner.Job != "build" || findings[0].IgnoreOrigin == nil || findings[0].IgnoreOrigin.Job != "build" {
					t.Fatalf("step finding lost its declaring job: %+v", findings)
				}
			}
		})
	}
}
