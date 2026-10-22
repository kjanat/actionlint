package tools_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestEmbeddedShellcheckRunner(t *testing.T) {
	t.Setenv("SHELLCHECK_OPTS", "--shell=sh")
	var called bool
	request := application.Options{Tools: (adapters.Options{ShellcheckRunner: func(args []string, script string) ([]byte, error) {
		called = true
		if !slices.Contains(args, "--norc") || slices.Contains(args, "-x") || !slices.Contains(args, "bash") {
			t.Errorf("embedded runner must disable filesystem reads and ignore host SHELLCHECK_OPTS: %q", args)
		}
		if strings.Contains(script, "${{") || !strings.Contains(script, "echo $value") {
			t.Errorf("runner did not receive prepared script: %q", script)
		}
		return []byte(`{"comments":[]}`), nil
	}}).Registrations()}
	var requestSources []workspace.Source
	var err error
	requestSources = []workspace.Source{{Path: "test.yml", InputPath: "test.yml", Content: []byte("on: push\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $value ${{ github.ref }}\n"), Project: nil}}
	_, err = testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("embedded checker was never called")
	}
}

func TestEmbeddedShellcheckFailure(t *testing.T) {
	for _, output := range []string{"invalid json", ""} {
		t.Run(output, func(t *testing.T) {
			request := application.Options{Tools: (adapters.Options{ShellcheckRunner: func([]string, string) ([]byte, error) {
				if output == "" {
					return nil, errors.New("worker timed out")
				}
				return []byte(output), nil
			}}).Registrations()}
			var requestSources []workspace.Source
			var err error
			requestSources = []workspace.Source{{Path: "test.yml", InputPath: "test.yml", Content: []byte("on: push\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"), Project: nil}}
			_, err = testutil.Diagnostics(application.Analyze(t.Context(), request, requestSources))
			if err == nil {
				t.Fatal("broken embedded checker must fail analysis")
			}
		})
	}
}
