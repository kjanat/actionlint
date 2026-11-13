package suspicious_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestUncancellableCondition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, jobIf, stepIf, uses string
		jobs, steps               int
	}{
		{"job", "${{ always() }}", "", "", 1, 0},
		{"bare job", "always()", "", "", 1, 0},
		{"guarded job", "${{ always() && !cancelled() }}", "", "", 0, 0},
		{"cancellation reporting", "${{ always() && cancelled() }}", "", "", 1, 0},
		{"job OR guard", "${{ always() || !cancelled() }}", "", "", 1, 0},
		{"checkout", "", "${{ always() }}", "actions/checkout@v6", 0, 0},
		{"checkout casing", "", "always()", "Actions/Checkout@v6", 0, 0},
		{"checkout SHA", "", "always()", "actions/checkout@" + strings.Repeat("a", 40), 0, 0},
		{"guarded checkout", "", "always() && !cancelled()", "actions/checkout@v6", 0, 0},
		{"step OR guard", "", "always() || !cancelled()", "actions/checkout@v6", 0, 0},
		{"log upload", "", "always()", "actions/upload-artifact@v4", 0, 0},
		{"run cleanup", "", "always()", "", 0, 0},
		{"other checkout", "", "always()", "example/checkout@v1", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n"
			if tc.jobIf != "" {
				source += "    if: " + tc.jobIf + "\n"
			}
			source += "    steps:\n      - "
			if tc.uses == "" {
				source += "run: echo cleanup\n"
			} else {
				source += "uses: " + tc.uses + "\n"
			}
			if tc.stepIf != "" {
				source += "        if: " + tc.stepIf + "\n"
			}
			ast, errs := workflowparser.Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			view := declaration.Publish(source, ast, false, "").Bind("test").Workflow()
			check := suspicious.UncancellableConditionCheck()
			for _, job := range view.Jobs() {
				got := check.Job(job)
				if len(got) != tc.jobs {
					t.Fatalf("job findings: %+v", got)
				}
				if strings.ContainsAny(tc.jobIf, "&|") && len(got) != 0 {
					if len(got[0].Fixes) != 0 || strings.Contains(got[0].Message, "!cancelled()") {
						t.Fatalf("compound condition received a substitution: %+v", got)
					}
				}
				for step := range job.Steps() {
					var got []reporting.Finding
					if check.Step != nil {
						got = check.Step(step)
					}
					if len(got) != tc.steps {
						t.Fatalf("step findings: %+v", got)
					}
				}
			}
		})
	}
}

func TestUncancellableConditionFix(t *testing.T) {
	t.Parallel()
	for _, scalar := range []string{
		"always()", "${{ always() }}", "'${{ always() }}'", `"${{ always() }}"`,
		`"${{ \u0061lways() }}"`, "&gate always()", "(always())",
		">-\n      always()", "${{ always( ) }}",
	} {
		t.Run(scalar, func(t *testing.T) {
			t.Parallel()
			content := "on: push\njobs:\n  build:\n    if: " + scalar + " # keep comment\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
			if strings.HasPrefix(scalar, ">") {
				content = strings.Replace(content, " # keep comment", "", 1)
			}
			if strings.HasPrefix(scalar, "&gate") {
				content = strings.Replace(content, "steps: [{run: echo ok}]", "steps: [{run: echo ok, if: *gate}]", 1)
			}
			ast, errs := workflowparser.Parse([]byte(content))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			view := declaration.Publish(content, ast, false, "").Bind("test").Workflow()
			for _, job := range view.Jobs() {
				findings := suspicious.UncancellableConditionCheck().Job(job)
				if len(findings) != 1 || len(findings[0].Fixes) != 1 {
					t.Fatalf("missing fix: %+v", findings)
				}
				fix := findings[0].Fixes[0]
				if fix.Applicability != results.FixUnsafe || fix.Applicability.Automatic() || len(fix.Edits) != 1 {
					t.Fatalf("unexpected fix: %+v", fix)
				}
				edit := fix.Edits[0]
				offset := func(pos sourcepos.Position) int {
					lines := strings.SplitAfter(content, "\n")
					prefix := strings.Join(lines[:pos.Line-1], "")
					return len(prefix) + len(string([]rune(lines[pos.Line-1])[:pos.Column-1]))
				}
				fixed := content[:offset(edit.Start)] + edit.Replacement + content[offset(edit.End):]
				if strings.Contains(content, "# keep comment") && !strings.Contains(fixed, "# keep comment") {
					t.Fatalf("lost comment: %s", fixed)
				}
				if strings.Contains(content, "&gate") && !strings.Contains(fixed, "&gate") {
					t.Fatalf("lost anchor: %s", fixed)
				}
				updated, parseErrs := workflowparser.Parse([]byte(fixed))
				if len(parseErrs) != 0 {
					t.Fatalf("invalid fix %s: %v", fixed, parseErrs)
				}
				checked := declaration.Publish(fixed, updated, false, "").Bind("fixed").Workflow()
				for _, fixedJob := range checked.Jobs() {
					if got := suspicious.UncancellableConditionCheck().Job(fixedJob); len(got) != 0 {
						t.Fatalf("fix leaves finding: %+v", got)
					}
					if !strings.Contains(fixedJob.If().Value(), "!cancelled()") {
						t.Fatalf("missing cancellation guard: %s", fixed)
					}
				}
			}
		})
	}
}

func TestCleanupStepCancellationTimeouts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, timeout, jobIf string
	}{
		{"no limit", "", ""},
		{"one minute", "1", ""},
		{"fraction", "2.5", ""},
		{"constant expression", "${{ 1 }}", ""},
		{"unknown expression", "${{ inputs.minutes }}", ""},
		{"job always", "1", "always()"},
		{"job cancellation only", "1", "cancelled()"},
		{"job guarded", "1", "${{ always() && !cancelled() }}"},
		{"job data predicate", "1", "github.ref == 'refs/heads/master'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n"
			if tc.timeout != "" {
				content += "    cancel-timeout-minutes: " + tc.timeout + "\n"
			}
			if tc.jobIf != "" {
				content += "    if: " + tc.jobIf + "\n"
			}
			content += "    steps:\n      - uses: actions/checkout@v6\n        if: always()\n"
			ast, errs := workflowparser.Parse([]byte(content))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			view := declaration.Publish(content, ast, false, "").Bind("test").Workflow()
			for _, job := range view.Jobs() {
				for step := range job.Steps() {
					check := suspicious.UncancellableConditionCheck()
					var findings []reporting.Finding
					if check.Step != nil {
						findings = check.Step(step)
					}
					if len(findings) != 0 {
						t.Fatalf("checkout needed for cleanup was classified by action name: %+v", findings)
					}
				}
			}
		})
	}
}
