package actionlint

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: |\n          " + comment
			if findings {
				workflow += "          echo $ACTIONLINT_STDIN_REGRESSION\n"
			}
			var errs []*Error
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
			if len(errs) != 1 || errs[0].Kind != "shellcheck" || !strings.Contains(errs[0].Message, "SC2086") || errs[0].Line != 8 {
				t.Fatalf("expected SC2086 on line 8 after the large comment: %s", output.String())
			}
		})
	}
}

func TestRuleShellcheckSanitizeExpressionsInScript(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{
		{
			"",
			"",
		},
		{
			"foo",
			"foo",
		},
		{
			"${{}}",
			"_____",
		},
		{
			"${{ matrix.foo }}",
			"_________________",
		},
		{
			"aaa ${{ matrix.foo }} bbb",
			"aaa _________________ bbb",
		},
		{
			"${{}}${{}}",
			"__________",
		},
		{
			"p${{a}}q${{b}}r",
			"p______q______r",
		},
		{
			"${{",
			"${{",
		},
		{
			"}}",
			"}}",
		},
		{
			"aaa${{foo",
			"aaa${{foo",
		},
		{
			"a${{b}}${{c",
			"a______${{c",
		},
		{
			"a${{b}}c}}d",
			"a______c}}d",
		},
		{
			"a}}b${{c}}d",
			"a}}b______d",
		},
		{
			"before ${{\nfoo\n}} after",
			"before ___\n___\n__ after",
		},
		{
			"before ${{\né\n}} after",
			"before ___\n_\n__ after",
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.input), func(t *testing.T) {
			have := sanitizeExpressionsInScript(tc.input)
			if tc.want != have {
				t.Fatalf("sanitized result is unexpected.\nwant: %q\nhave: %q", tc.want, have)
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
			r := newRuleShellcheck(&externalCommand{})

			w := &Workflow{}
			if tc.workflow != "" {
				w.Defaults = &Defaults{
					Run: &DefaultsRun{
						Shell: &String{Value: tc.workflow},
					},
				}
			}
			if err := r.VisitWorkflowPre(w); err != nil {
				t.Fatal(err)
			}

			j := &Job{}
			if tc.job != "" {
				j.Defaults = &Defaults{
					Run: &DefaultsRun{
						Shell: &String{Value: tc.job},
					},
				}
			}
			if tc.runner != "" {
				j.RunsOn = &Runner{
					Labels: []*String{
						{Value: tc.runner},
					},
				}
			}
			if err := r.VisitJobPre(j); err != nil {
				t.Fatal(err)
			}

			e := &ExecRun{}
			if tc.step != "" {
				e.Shell = &String{Value: tc.step}
			}
			if s := r.getShellName(e); s != tc.want {
				t.Fatalf("detected shell %q but wanted %q", s, tc.want)
			}
		})
	}
}
