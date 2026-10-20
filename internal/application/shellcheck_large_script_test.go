package application_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools/adapters"

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
			dir := t.TempDir()
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
			var result *application.Result
			done := make(chan error, 1)
			go func() {
				var err error
				result, err = application.Check(ctx, application.Options{WorkingDir: dir, Tools: adapters.Options{ShellCheck: shellcheck}.Registrations()}, application.Input{Kind: application.Stdin, Name: filepath.Join(dir, "workflow.yml"), Reader: strings.NewReader(workflow)})
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
				if len(result.Diagnostics) != 0 {
					t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || !strings.Contains(result.Diagnostics[0].Message, "SC2086") || result.Diagnostics[0].Start.Line != 8 {
				t.Fatalf("expected SC2086 on line 8 after the large comment: %+v", result.Diagnostics)
			}
		})
	}
}
