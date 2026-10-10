package cli

import (
	"os"
	"strings"
	"testing"
)

func TestRulePresetFlags(t *testing.T) {
	const source = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    steps:
      - uses: actions/checkout@v6
`
	for _, modern := range []bool{false, true} {
		for _, tc := range []struct {
			flags  []string
			strict bool
		}{
			{nil, false}, {[]string{"--strict"}, true},
			{[]string{"--experimental"}, false}, {[]string{"--strict", "--experimental"}, true},
			{[]string{"--strict", "--experimental=false"}, true},
		} {
			args := []string{"--no-config"}
			if modern {
				args = append([]string{"check"}, args...)
			}
			args = append(args, tc.flags...)
			args = append(args, "-")
			got := testRunCommand(source, args...)
			wantStatus := 0
			if tc.strict {
				wantStatus = 1
			}
			if got.Status != wantStatus {
				t.Fatalf("%v: %+v", args, got)
			}
			for _, rule := range []string{"require-commit-hash", "require-job-timeout", "require-permissions"} {
				if strings.Contains(got.Stdout, "["+rule+"]") != tc.strict {
					t.Fatalf("%v: %+v", args, got)
				}
			}
			if strings.Contains(got.Stdout, "[case-insensitive-conditions]") != tc.strict {
				t.Fatalf("%v: %+v", args, got)
			}
		}
	}
}

func TestRulePresetEnvironment(t *testing.T) {
	for _, variable := range []string{"ACTIONLINT_STRICT", "ACTIONLINT_EXPERIMENTAL"} {
		t.Run(variable, func(t *testing.T) {
			flag := "--" + strings.ToLower(strings.TrimPrefix(variable, "ACTIONLINT_"))
			source := strings.Replace(commandGoodWorkflow, "    steps:", "    if: github.ref == 'refs/heads/main'\n    steps:", 1)
			t.Setenv(variable, "true")
			for _, prefix := range [][]string{nil, {"check"}} {
				args := append(append([]string{}, prefix...), "--no-config", "-")
				want := 0
				if variable == "ACTIONLINT_STRICT" {
					want = 1
				}
				if got := testRunCommand(source, args...); got.Status != want {
					t.Fatal(got)
				}
				args = append(append([]string{}, prefix...), "--no-config", flag+"=false", "-")
				if got := testRunCommand(source, args...); got.Status != 0 {
					t.Fatal(got)
				}
			}
			t.Setenv(variable, "invalid")
			if got := testRunCommand(source, "check", "--no-config", "-"); got.Status != 2 {
				t.Fatal(got)
			}
			if got := testRunCommand("", "version"); got.Status != 0 {
				t.Fatal(got)
			}
			if got := testRunCommand(source, "check", "--no-config", flag+"=false", "-"); got.Status != 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestExperimentalFlagDoesNotDisableStableRules(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.yml", []byte("lint: {rules: {suspicious: {case-insensitive-conditions: on}}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(commandGoodWorkflow, "    steps:", "    if: github.ref == 'refs/heads/main'\n    steps:", 1)
	for _, prefix := range [][]string{nil, {"check"}} {
		args := append(append([]string{}, prefix...), "--config=config.yml", "--experimental=false", "-")
		if got := testRunCommand(source, args...); got.Status != 1 {
			t.Fatal(got)
		}
	}
}
