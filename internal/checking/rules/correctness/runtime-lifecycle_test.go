package correctness

import (
	"fmt"
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/model/services"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func TestRuntimeLifecycleSetupNode(t *testing.T) {
	t.Parallel()
	date, err := lifecycle.ParseDate("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	service, err := services.ParseService("github.com", "")
	if err != nil {
		t.Fatal(err)
	}
	check := RuntimeLifecycleChecks(actionmeta.LifecycleContext{AsOf: date, Service: service})
	for _, tc := range []struct {
		uses, version string
		count         int
		current       bool
	}{
		{"actions/setup-node@v4", "12", 1, true},
		{"actions/setup-node@v4", " 16 ", 1, true},
		{"actions/setup-node@0123456789012345678901234567890123456789", "16.x", 1, true},
		{"actions/setup-node@v4", "22", 1, false},
		{"actions/setup-node@v4", "26", 1, false},
		{"ACTIONS/setup-node@v4", "26", 1, false},
		{"actions/setup-node/subpath@v4", "12", 0, false},
		{"other/setup-node@v4", "12", 0, false},
		{"actions/setup-node@v4", "lts/*", 0, false},
		{"actions/setup-node@v4", "${{ inputs.node }}", 0, false},
		{"actions/setup-node@v4", ">=20", 0, false},
	} {
		t.Run(tc.uses+"/"+tc.version, func(t *testing.T) {
			t.Parallel()
			text := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: %s\n        with:\n          node-version: '%s'\n", tc.uses, tc.version)
			parsed, errs := workflowparser.ParseWorkflow([]byte(text))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			workflow := workflowdecl.Bind("runtime-lifecycle", text, parsed, false, "").Workflow()
			job, ok := workflow.Job("test")
			if !ok {
				t.Fatal("missing job")
			}
			for step := range job.Steps() {
				findings := check.Step(step)
				if len(findings) != tc.count {
					t.Fatalf("got %d findings, want %d: %+v", len(findings), tc.count, findings)
				}
				if len(findings) == 0 {
					continue
				}
				f := findings[0]
				if f.LifecycleEvidence == nil || f.LifecycleEvidence.Current != tc.current || f.LifecycleEvidence.Subject != lifecycle.LanguageRuntimes || f.Start.Line != 8 || f.LifecycleEvidence.AsOf != date {
					t.Fatalf("invalid evidence or source position: %+v", f)
				}
			}
		})
	}
}

func TestRuntimeLifecycleSetupNodeLiteralBlock(t *testing.T) {
	t.Parallel()
	date, err := lifecycle.ParseDate("2026-11-15")
	if err != nil {
		t.Fatal(err)
	}
	text := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/setup-node@v4\n        with:\n          node-version: |\n            16\n"
	parsed, errs := workflowparser.ParseWorkflow([]byte(text))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	workflow := workflowdecl.Bind("runtime-lifecycle", text, parsed, false, "").Workflow()
	job, ok := workflow.Job("test")
	if !ok {
		t.Fatal("missing job")
	}
	check := RuntimeLifecycleChecks(actionmeta.LifecycleContext{AsOf: date})
	for step := range job.Steps() {
		findings := check.Step(step)
		if len(findings) != 1 || findings[0].LifecycleEvidence == nil || findings[0].LifecycleEvidence.Name != "node16" || !findings[0].LifecycleEvidence.Current {
			t.Fatalf("expected retired Node.js 16 finding, got %+v", findings)
		}
	}
}

func TestRuntimeLifecycleOnReleaseWorkflow(t *testing.T) {
	t.Parallel()
	// Source: kjanat/actionlint .github/workflows/release.yml at
	// 8d8c9782a68f5ca91fd16719f7baddc26a3dd1d2.
	content, err := os.ReadFile("testdata/runtime-lifecycle-release.yml")
	if err != nil {
		t.Fatal(err)
	}
	parsed, errs := workflowparser.ParseWorkflow(content)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	workflow := workflowdecl.Bind("release-workflow", string(content), parsed, false, "").Workflow()
	job, ok := workflow.Job("verify")
	if !ok {
		t.Fatal("missing release verification job")
	}
	for _, day := range []string{"2026-11-15", "2028-04-30"} {
		date, err := lifecycle.ParseDate(day)
		if err != nil {
			t.Fatal(err)
		}
		check := RuntimeLifecycleChecks(actionmeta.LifecycleContext{AsOf: date})
		count := 0
		for step := range job.Steps() {
			for _, f := range check.Step(step) {
				count++
				e := f.LifecycleEvidence
				if e == nil || e.Name != "node24" || e.Effective.String() != "2028-04-30" || e.Current != (day == "2028-04-30") || f.Start.Line != 30 {
					t.Fatalf("unexpected release workflow evidence: %+v", f)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s: got %d lifecycle observations, want 1", day, count)
		}
	}
}
