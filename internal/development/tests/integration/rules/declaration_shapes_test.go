package rules_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/checking/rules/suspicious"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func shapeFindings(t *testing.T, src string) []reporting.Finding {
	t.Helper()
	ast, _ := workflowparser.Parse([]byte(src))
	w := declaration.Publish(src, ast, false, "").Bind("test").Workflow()
	out := suspicious.EmptyTriggers(w)
	for _, job := range w.Jobs() {
		out = append(out, correctness.EmptyJob(job)...)
		out = append(out, correctness.NumericValues(job)...)
		for step := range job.Steps() {
			out = append(out, suspicious.EmptyRun(step)...)
		}
	}
	return out
}

func TestShapeChecks(t *testing.T) {
	t.Parallel()
	const steps = "    steps:\n      - run: echo ok\n"
	for _, tc := range []struct {
		name, src string
		want      []string
	}{
		{"complete", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n" + steps, nil},
		{"empty on", "on:\njobs:\n  a:\n    runs-on: ubuntu-latest\n" + steps, []string{"empty-triggers/"}},
		{"empty on mapping", "on: {}\njobs:\n  a:\n    runs-on: ubuntu-latest\n" + steps, []string{"empty-triggers/"}},
		// Invalid events already fail syntax-check; they are not also empty.
		{"invalid on", "on: [schedule]\njobs:\n  a:\n    runs-on: ubuntu-latest\n" + steps, nil},
		{"missing on", "jobs:\n  a:\n    runs-on: ubuntu-latest\n" + steps, nil},
		{"no steps", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n", []string{"empty-job/"}},
		{"empty steps", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps: []\n", []string{"empty-job/"}},
		{"reusable call", "on: push\njobs:\n  a:\n    uses: ./.github/workflows/called.yml\n", nil},
		{"empty run", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run:\n", []string{"empty-run/"}},
		{"non-string run", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: [a]\n", nil},
		{"truncated", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    strategy:\n      max-parallel: 1.5\n" + steps, []string{"numeric-values/truncated"}},
		{"truncated to zero", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    strategy:\n      max-parallel: .5\n" + steps, []string{"numeric-values/truncated", "numeric-values/non-positive"}},
		{"zero", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    strategy:\n      max-parallel: 0\n" + steps, []string{"numeric-values/non-positive"}},
		{"expression", "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    strategy:\n      max-parallel: ${{ 0 }}\n" + steps, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, finding := range shapeFindings(t, tc.src) {
				severity := "warning"
				if finding.Rule == "empty-job" {
					severity = "error"
				}
				if finding.Severity != severity {
					t.Errorf("%s severity = %q, want %q", finding.Rule, finding.Severity, severity)
				}
				got = append(got, finding.Rule+"/"+finding.Code)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("findings = %q, want %q", got, tc.want)
			}
		})
	}
}
