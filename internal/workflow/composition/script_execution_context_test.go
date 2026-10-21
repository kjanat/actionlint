package composition

import (
	"testing"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
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
			collector := &compositeAnalyzer{collectScripts: true, resolveScriptExecution: true, actions: actionlocal.NewResolver(nil, nil)}

			w := &workflowlang.Workflow{}
			if tc.workflow != "" {
				w.Defaults = &workflowlang.Defaults{
					Run: &workflowlang.DefaultsRun{
						Shell: &workflowlang.String{Value: tc.workflow},
					},
				}
			}
			collector.beginWorkflow(w)

			j := &workflowlang.Job{}
			if tc.job != "" {
				j.Defaults = &workflowlang.Defaults{
					Run: &workflowlang.DefaultsRun{
						Shell: &workflowlang.String{Value: tc.job},
					},
				}
			}
			if tc.runner != "" {
				j.RunsOn = &workflowlang.Runner{
					Labels: []*workflowlang.String{
						{Value: tc.runner},
					},
				}
			}
			collector.beginJob(j)

			e := &workflowlang.ExecRun{}
			if tc.step != "" {
				e.Shell = &workflowlang.String{Value: tc.step}
			}
			e.Run = &workflowlang.String{Value: "echo ok"}
			if err := collector.collectScript(&workflowlang.Step{Exec: e}, nil, "", nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			if s := collector.scripts[0].Execution.Shell.Name; s != tc.want {
				t.Fatalf("detected shell %q but wanted %q", s, tc.want)
			}
		})
	}
}
