package cli_test

import (
	"os"
	"strings"
	"testing"
)

func TestNoConfigAliases(t *testing.T) {
	t.Chdir(commandTestRepo(t))
	if err := os.WriteFile(".github/actionlint.yaml", []byte("invalid: [yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_CONFIG", "missing-env-config.yml")
	t.Setenv("ACTIONLINT_NO_CONFIG", "not-a-boolean")
	for _, flag := range []string{"--no-config", "--no-rc", "--norc"} {
		for _, args := range [][]string{
			{flag, "-"},
			{"check", flag, "-"},
			{flag, "check", "-"},
			{"doctor", flag, "--json"},
			{"config", "show", flag, "--json"},
		} {
			got := testRunCommand(commandGoodWorkflow, args...)
			if got.Status != 0 || got.Stderr != "" {
				t.Fatalf("config was loaded or environment overrode %v: %+v", args, got)
			}
		}
		got := testRunCommand(commandGoodWorkflow, "check", flag, "--config=explicit.yml", "-")
		if got.Status != 2 || !strings.Contains(got.Stderr, "cannot be combined") {
			t.Fatalf("config conflict changed for %s: %+v", flag, got)
		}
	}
}

func TestNoConfigAliasFalseOverridesEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("settings.yml", []byte("lint: {rules: {correctness: {expression-reference: off}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_CONFIG", "missing-env-config.yml")
	t.Setenv("ACTIONLINT_NO_CONFIG", "true")
	for _, flag := range []string{"--no-config", "--no-rc", "--norc"} {
		got := testRunCommand(commandBadWorkflow, "check", flag+"=false", "--config=settings.yml", "-")
		if got.Status != 0 {
			t.Fatalf("explicit false ignored for %s: %+v", flag, got)
		}
		got = testRunCommand(commandBadWorkflow, "check", flag+"=false", "-")
		if got.Status != 1 || !strings.Contains(got.Stdout, "undefined variable") {
			t.Fatalf("explicit false did not override configuration environment for %s: %+v", flag, got)
		}
	}
}
