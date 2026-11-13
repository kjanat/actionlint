package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	"github.com/google/go-cmp/cmp"
)

func TestCachePolicyCommandOutput(t *testing.T) {
	t.Parallel()
	for _, exception := range []string{"", " # actionlint:ignore cache-write-untrusted -- reviewed code only"} {
		source := "on: pull_request_target\ncache-mode: write" + exception + `
jobs:
  test:
` + cachePolicySteps + "\npermissions: {}\n"
		var stdout, stderr strings.Builder
		cmd := cli.Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
		status := cmd.Main([]string{"actionlint", "--summary=false", "-no-tool=shellcheck", "-no-tool=ruff", "-no-color", "-no-config", "-format", "{{json .}}", "-"})
		if stderr.Len() != 0 {
			t.Fatalf("unexpected stderr: %s", &stderr)
		}
		var diagnostics []struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
			t.Fatal(err)
		}
		if exception == "" {
			if status != commandstatus.ExitSuccessProblemFound || len(diagnostics) != 1 || diagnostics[0].Kind != "cache-write-untrusted" {
				t.Fatalf("status=%d diagnostics=%v", status, diagnostics)
			}
		} else if status != commandstatus.ExitSuccessNoProblem || len(diagnostics) != 0 {
			t.Fatalf("status=%d diagnostics=%v", status, diagnostics)
		}
	}
}

func TestCachePolicyInvalidSuppressionSARIF(t *testing.T) {
	t.Parallel()
	source := `on: pull_request_target
cache-mode: write # actionlint:ignore cache-write-untrusted --
jobs:
  test:
` + cachePolicySteps + "\npermissions: {}\n"
	var stdout, stderr strings.Builder
	cmd := cli.Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
	status := cmd.Main([]string{"actionlint", "--summary=false", "-no-tool=shellcheck", "-no-tool=ruff", "-no-config", "-output", "sarif", "-"})
	if status != commandstatus.ExitSuccessProblemFound || stderr.Len() != 0 {
		t.Fatalf("status=%d stderr=%s", status, &stderr)
	}
	var report struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Rules []struct{ ID string }
				}
			}
			Results []struct{ RuleID string }
		}
	}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 1 || len(report.Runs[0].Results) != 2 {
		t.Fatalf("expected policy and directive results: %s", &stdout)
	}
	descriptors := map[string]bool{}
	for _, rule := range report.Runs[0].Tool.Driver.Rules {
		descriptors[rule.ID] = true
	}
	for _, result := range report.Runs[0].Results {
		if !descriptors[result.RuleID] {
			t.Errorf("SARIF result %q has no rule descriptor", result.RuleID)
		}
	}
}

func TestCachePolicySuppressionFilterPrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, directive, cliIgnore, pathIgnore string
		kinds                                  []string
	}{
		{"redundant CLI ignore", "cache-write-untrusted -- reviewed", "poison caches", "", nil},
		{"CLI ignore keeps directive errors", "typo -- reviewed", "poison caches", "", []string{"inline-suppression"}},
		{"CLI ignore removes directive errors", "typo -- reviewed", "unknown inline suppression rule", "", []string{"cache-write-untrusted"}},
		{"redundant path ignore", "cache-write-untrusted -- reviewed", "", "poison caches", nil},
		{"path ignore keeps directive errors", "typo -- reviewed", "", "poison caches", []string{"inline-suppression"}},
		{"path ignore removes directive errors", "typo -- reviewed", "", "unknown inline suppression rule", []string{"cache-write-untrusted"}},
		{"all filters overlap", "cache-operation,cache-write-untrusted -- reviewed", "poison caches", "poison caches", []string{"unused-suppression"}},
		{"separate filters remove both errors", "typo -- reviewed", "poison caches", "unknown inline suppression rule", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if tc.pathIgnore != "" {
				if err := os.WriteFile(config, []byte(`lint:
  message-ignores:
    'test.yaml': ['`+tc.pathIgnore+"']\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			source := "on: pull_request_target\ncache-mode: write # actionlint:ignore " + tc.directive + `
jobs:
  test:
` + cachePolicySteps + "\npermissions: {}\n"
			var stdout, stderr strings.Builder
			cmd := cli.Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			args := []string{"actionlint", "--summary=false", "-no-tool=shellcheck", "-no-tool=ruff", "-no-color", "-format", "{{json .}}", "-stdin-filename", "test.yaml"}
			if tc.pathIgnore != "" {
				args = append(args, "-config-file", config)
			} else {
				args = append(args, "-no-config")
			}
			if tc.cliIgnore != "" {
				args = append(args, "--ignore-regex", tc.cliIgnore)
			}
			status := cmd.Main(append(args, "-"))
			wantStatus := commandstatus.ExitSuccessNoProblem
			if len(tc.kinds) > 0 {
				wantStatus = commandstatus.ExitSuccessProblemFound
			}
			if status != wantStatus || stderr.Len() != 0 {
				t.Fatalf("status=%d, want %d; stderr=%s", status, wantStatus, &stderr)
			}
			var diagnostics []struct{ Kind string }
			if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, diagnostic := range diagnostics {
				kinds = append(kinds, diagnostic.Kind)
			}
			if diff := cmp.Diff(tc.kinds, kinds); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
