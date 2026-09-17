package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
	"github.com/google/go-cmp/cmp"
)

func TestCachePolicyCommandOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, exception := range []string{"", " # actionlint:ignore cache-write-untrusted -- reviewed code only"} {
		source := "on: pull_request_target\ncache-mode: write" + exception + "\njobs:\n  test:\n" + cachePolicySteps
		var stdout, stderr strings.Builder
		cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
		status := cmd.Main([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", "{{json .}}", "-"})
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
			if status != actionlint.ExitStatusSuccessProblemFound || len(diagnostics) != 1 || diagnostics[0].Kind != "cache-write-untrusted" {
				t.Fatalf("status=%d diagnostics=%v", status, diagnostics)
			}
		} else if status != actionlint.ExitStatusSuccessNoProblem || len(diagnostics) != 0 {
			t.Fatalf("status=%d diagnostics=%v", status, diagnostics)
		}
	}
}

func TestCachePolicyInvalidSuppressionSARIF(t *testing.T) {
	t.Chdir(t.TempDir())
	source := "on: pull_request_target\ncache-mode: write # actionlint:ignore cache-write-untrusted\njobs:\n  test:\n" + cachePolicySteps
	var stdout, stderr strings.Builder
	cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
	status := cmd.Main([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-format", actionlint.SARIFTemplate(), "-"})
	if status != actionlint.ExitStatusSuccessProblemFound || stderr.Len() != 0 {
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
		{"all filters overlap", "cache-operation,cache-write-untrusted -- reviewed", "poison caches", "poison caches", nil},
		{"separate filters remove both errors", "typo -- reviewed", "poison caches", "unknown inline suppression rule", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if tc.pathIgnore != "" {
				if err := os.WriteFile(config, []byte("paths:\n  'test.yaml':\n    ignore: ['"+tc.pathIgnore+"']\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			source := "on: pull_request_target\ncache-mode: write # actionlint:ignore " + tc.directive + "\njobs:\n  test:\n" + cachePolicySteps
			var stdout, stderr strings.Builder
			cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", "{{json .}}", "-stdin-filename", "test.yaml"}
			if tc.pathIgnore != "" {
				args = append(args, "-config-file", config)
			}
			if tc.cliIgnore != "" {
				args = append(args, "-ignore", tc.cliIgnore)
			}
			status := cmd.Main(append(args, "-"))
			wantStatus := actionlint.ExitStatusSuccessNoProblem
			if len(tc.kinds) > 0 {
				wantStatus = actionlint.ExitStatusSuccessProblemFound
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
