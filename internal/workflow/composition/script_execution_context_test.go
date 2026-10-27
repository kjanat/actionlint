package composition

import (
	"fmt"
	"strings"
	"testing"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
)

// Regression for #409
func TestRuleShellcheckDetectShell(t *testing.T) {
	tests := []struct {
		what     string
		want     string
		workflow string // Shell name set at 'defaults' in Workflow node
		job      string // Shell name set at 'defaults' in Job node
		step     string // Shell name set at 'shell' in Step node
		runner   string // Runner name at 'runs-on' in Job node
	}{
		{
			what: "no default shell",
			want: "bash",
		},
		{
			what:     "workflow default",
			want:     "pwsh",
			workflow: "pwsh",
		},
		{
			what: "job default",
			want: "pwsh",
			job:  "pwsh",
		},
		{
			what: "step config",
			want: "pwsh",
			step: "pwsh",
		},
		{
			what:     "job default is more proioritized than workflow",
			want:     "pwsh",
			workflow: "bash",
			job:      "pwsh",
		},
		{
			what:     "step config is more proioritized than job",
			want:     "pwsh",
			workflow: "sh",
			job:      "bash",
			step:     "pwsh",
		},
		{
			what:   "default shell detected from runner",
			want:   "pwsh",
			runner: "windows-latest",
		},
		{
			what:     "workflow default is more proioritized than runner",
			want:     "bash",
			workflow: "bash",
			runner:   "windows-latest",
		},
		{
			what:   "job default is more proioritized than runner",
			want:   "bash",
			job:    "bash",
			runner: "windows-latest",
		},
		{
			what:   "step config is more proioritized than runner",
			want:   "bash",
			step:   "bash",
			runner: "windows-latest",
		},
		{
			what:   "no shell is detected from Ubuntu runner",
			want:   "bash",
			runner: "ubuntu-latest",
		},
		{
			what:     "custom bash",
			want:     "bash -e {0}",
			workflow: "bash -e {0}",
		},
		{
			what:     "custom sh",
			want:     "sh -e {0}",
			workflow: "sh -e {0}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			collector := &compositeAnalyzer{compositionSession: &compositionSession{collectScripts: true, resolveScriptExecution: true}, actions: actionlocal.NewResolver(nil, nil)}

			var source strings.Builder
			source.WriteString("on: push\n")
			if tc.workflow != "" {
				fmt.Fprintf(&source, "defaults: {run: {shell: %q}}\n", tc.workflow)
			}
			source.WriteString("jobs:\n  test:\n")
			if tc.job != "" {
				fmt.Fprintf(&source, "    defaults: {run: {shell: %q}}\n", tc.job)
			}
			if tc.runner != "" {
				fmt.Fprintf(&source, "    runs-on: %q\n", tc.runner)
			}
			source.WriteString("    steps:\n      - run: echo ok\n")
			if tc.step != "" {
				fmt.Fprintf(&source, "        shell: %q\n", tc.step)
			}
			content := []byte(source.String())
			document := documentparse.Workflow("fixture.yml", content)
			w := document.Workflow
			if w == nil {
				t.Fatal(document.Findings)
			}
			j := w.Jobs["test"]
			step := j.Steps[0]
			startWorkflowEntry(t, collector, workflowSnapshot(t, content, document))
			if err := collector.beginWorkflow(w); err != nil {
				t.Fatal(err)
			}
			if err := collector.beginJob(j); err != nil {
				t.Fatal(err)
			}
			if err := collector.collectScript(step, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			if s := collector.scripts[0].Execution.Shell.Name; s != tc.want {
				t.Fatalf("detected shell %q but wanted %q", s, tc.want)
			}
		})
	}
}
