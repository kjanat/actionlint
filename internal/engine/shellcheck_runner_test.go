package engine

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestEmbeddedShellcheckRunner(t *testing.T) {
	t.Setenv("SHELLCHECK_OPTS", "--shell=sh")
	var called bool
	request := Request{Tools: (adapters.Options{ShellcheckRunner: func(args []string, script string) ([]byte, error) {
		called = true
		if !slices.Contains(args, "--norc") || slices.Contains(args, "-x") || !slices.Contains(args, "bash") {
			t.Errorf("embedded runner must disable filesystem reads and ignore host SHELLCHECK_OPTS: %q", args)
		}
		if strings.Contains(script, "${{") || !strings.Contains(script, "echo $value") {
			t.Errorf("runner did not receive prepared script: %q", script)
		}
		return []byte(`{"comments":[]}`), nil
	}}).Registrations()}
	var err error

	request.Sources = []SourceUnit{{Path: "test.yml", InputPath: "test.yml", Content: []byte("on: push\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $value ${{ github.ref }}\n"), Project: nil}}
	_, err = testFindings(runTestAnalysis(t.Context(), request))
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
			request := Request{Tools: (adapters.Options{ShellcheckRunner: func([]string, string) ([]byte, error) {
				if output == "" {
					return nil, errors.New("worker timed out")
				}
				return []byte(output), nil
			}}).Registrations()}
			var err error

			request.Sources = []SourceUnit{{Path: "test.yml", InputPath: "test.yml", Content: []byte("on: push\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"), Project: nil}}
			_, err = testFindings(runTestAnalysis(t.Context(), request))
			if err == nil {
				t.Fatal("broken embedded checker must fail analysis")
			}
		})
	}
}
