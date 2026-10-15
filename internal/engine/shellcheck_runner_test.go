package engine

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestEmbeddedShellcheckRunner(t *testing.T) {
	t.Setenv("SHELLCHECK_OPTS", "--shell=sh")
	var called bool
	linter, err := NewLinter(io.Discard, &LinterOptions{
		OnRulesCreated: func(rules []Rule) []Rule {
			return append(rules, NewRuleShellcheckWithRunner(func(args []string, script string) ([]byte, error) {
				called = true
				if !slices.Contains(args, "--norc") || slices.Contains(args, "-x") || !slices.Contains(args, "bash") {
					t.Errorf("embedded runner must disable filesystem reads and ignore host SHELLCHECK_OPTS: %q", args)
				}
				if strings.Contains(script, "${{") || !strings.Contains(script, "echo $value") {
					t.Errorf("runner did not receive prepared script: %q", script)
				}
				return []byte(`{"comments":[]}`), nil
			}))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = linter.Lint("test.yml", []byte("on: push\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $value ${{ github.ref }}\n"), nil)
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
			linter, err := NewLinter(io.Discard, &LinterOptions{
				OnRulesCreated: func(rules []Rule) []Rule {
					return append(rules, NewRuleShellcheckWithRunner(func([]string, string) ([]byte, error) {
						if output == "" {
							return nil, errors.New("worker timed out")
						}
						return []byte(output), nil
					}))
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = linter.Lint("test.yml", []byte("on: push\njobs:\n  t:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"), nil)
			if err == nil {
				t.Fatal("broken embedded checker must fail analysis")
			}
		})
	}
}
