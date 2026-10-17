package engine

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	actionlocal "actionlint.kjanat.dev/internal/action/local"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"

	"golang.org/x/sys/execabs"
)

func TestRuleShellcheckLargeRunBlock(t *testing.T) {
	shellcheck, err := execabs.LookPath("shellcheck")
	if err != nil {
		t.Skipf("shellcheck is necessary to run this test: %s", err)
	}
	// @kjanat's rhysd/actionlint#651 reproducer uses a single 128 KiB comment.
	// The second case proves ShellCheck still checks code following that comment.
	comment := "#" + strings.Repeat("x", 128*1024-2) + "\n"
	for _, findings := range []bool{false, true} {
		t.Run(fmt.Sprintf("findings=%t", findings), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var output bytes.Buffer
			dir := t.TempDir()
			l, err := NewLinter(&output, &LinterOptions{Shellcheck: shellcheck, WorkingDir: dir, Context: ctx, Color: ColorOptionKindNever})
			if err != nil {
				t.Fatal(err)
			}
			workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: |
          ` + comment
			if findings {
				workflow += "          echo $ACTIONLINT_STDIN_REGRESSION\n"
			}
			var errs []*findingmodel.Finding
			done := make(chan error, 1)
			go func() {
				var err error
				errs, err = l.Lint(filepath.Join(dir, "workflow.yml"), []byte(workflow), nil)
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("ShellCheck did not finish checking the large run block")
			}
			if !findings {
				if len(errs) != 0 {
					t.Fatalf("unexpected diagnostics: %s", output.String())
				}
				return
			}
			if len(errs) != 1 || errs[0].Rule != "shellcheck" || !strings.Contains(errs[0].Message, "SC2086") || errs[0].Start.Line != 8 {
				t.Fatalf("expected SC2086 on line 8 after the large comment: %s", output.String())
			}
		})
	}
}

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
			collector.collectScript(&workflowlang.Step{Exec: e}, nil, "", nil, nil, nil)
			if s := collector.scripts[0].execution.shell.Name; s != tc.want {
				t.Fatalf("detected shell %q but wanted %q", s, tc.want)
			}
		})
	}
}
