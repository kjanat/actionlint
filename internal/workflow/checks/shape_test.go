package checks

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

func shapeFindings(t *testing.T, src string) []Finding {
	t.Helper()
	ast, _ := workflowparser.Parse([]byte(src))
	w := declaration.Publish(src, ast, false, "").Bind("test").Workflow()
	out := EmptyTriggers(w)
	for _, job := range w.Jobs() {
		out = append(out, EmptyJob(job)...)
		out = append(out, NumericValues(job)...)
		for step := range job.Steps() {
			out = append(out, EmptyRun(step)...)
		}
	}
	return out
}

func TestShapeChecks(t *testing.T) {
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
			var got []string
			for _, finding := range shapeFindings(t, tc.src) {
				if finding.Severity != "warning" {
					t.Errorf("%s is not a warning", finding.Rule)
				}
				got = append(got, finding.Rule+"/"+finding.Code)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("findings = %q, want %q", got, tc.want)
			}
		})
	}
}
