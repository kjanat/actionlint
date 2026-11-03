package checks

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

func TestUncancellableCondition(t *testing.T) {
	for _, tc := range []struct {
		name, jobIf, stepIf, uses string
		jobs, steps               int
	}{
		{"job", "${{ always() }}", "", "", 1, 0},
		{"bare job", "always()", "", "", 1, 0},
		{"guarded job", "${{ always() && !cancelled() }}", "", "", 0, 0},
		{"checkout", "", "${{ always() }}", "actions/checkout@v6", 0, 1},
		{"checkout casing", "", "always()", "Actions/Checkout@v6", 0, 1},
		{"checkout SHA", "", "always()", "actions/checkout@" + strings.Repeat("a", 40), 0, 1},
		{"guarded checkout", "", "always() && !cancelled()", "actions/checkout@v6", 0, 0},
		{"ineffective OR guard", "", "always() || !cancelled()", "actions/checkout@v6", 0, 1},
		{"log upload", "", "always()", "actions/upload-artifact@v4", 0, 0},
		{"run cleanup", "", "always()", "", 0, 0},
		{"other checkout", "", "always()", "example/checkout@v1", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			check := Declaration("uncancellable-condition")
			for _, job := range view.Jobs() {
				if got := check.Job(job); len(got) != tc.jobs {
					t.Fatalf("job findings: %+v", got)
				}
				for step := range job.Steps() {
					got := check.Step(step)
					if len(got) != tc.steps {
						t.Fatalf("step findings: %+v", got)
					}
					for _, finding := range got {
						if finding.Severity != "warning" || !finding.Step.Present() {
							t.Fatalf("unexpected diagnostic: %+v", finding)
						}
					}
				}
			}
		})
	}
}

func TestUncancellableConditionFix(t *testing.T) {
	for _, scalar := range []string{
		"always()", "${{ always() }}", "'${{ always() }}'", `"${{ always() }}"`,
		`"${{ \u0061lways() }}"`, "&gate always()", "(always())",
		">-\n      always()", "${{ always( ) }}",
	} {
		t.Run(scalar, func(t *testing.T) {
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
				findings := Declaration("uncancellable-condition").Job(job)
				if len(findings) != 1 || len(findings[0].Fixes) != 1 {
					t.Fatalf("missing fix: %+v", findings)
				}
				fix := findings[0].Fixes[0]
				if fix.Applicability != diagnostic.FixSuggested || len(fix.Edits) != 1 {
					t.Fatalf("unexpected fix: %+v", fix)
				}
				edit := fix.Edits[0]
				offset := func(pos diagnostic.Position) int {
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
					if got := Declaration("uncancellable-condition").Job(fixedJob); len(got) != 0 {
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

func TestCheckoutCancellationTimeoutSeverity(t *testing.T) {
	for _, tc := range []struct {
		name, timeout, jobIf, severity string
	}{
		{"no limit", "", "", "warning"},
		{"one minute", "1", "", "info"},
		{"fraction", "2.5", "", "info"},
		{"constant expression", "${{ 1 }}", "", "info"},
		{"unknown expression", "${{ inputs.minutes }}", "", "warning"},
		{"zero", "0", "", "warning"},
		{"negative", "-1", "", "warning"},
		{"job always", "1", "always()", "warning"},
		{"job cancellation only", "1", "cancelled()", "warning"},
		{"job guarded", "1", "${{ always() && !cancelled() }}", "info"},
		{"job data predicate", "1", "github.ref == 'refs/heads/master'", "info"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
					findings := Declaration("uncancellable-condition").Step(step)
					if len(findings) != 1 || findings[0].Severity != tc.severity {
						t.Fatalf("want %s: %+v", tc.severity, findings)
					}
				}
			}
		})
	}
}
