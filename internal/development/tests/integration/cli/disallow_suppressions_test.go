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

func TestDisallowSuppressionsCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config string
		kinds        []string
	}{
		{"absent", "", nil},
		{"empty policy", "lint: {rules: {policy: {}}}", nil},
		{"null policy", "lint: {rules: {policy: null}}", nil},
		{"null", "lint: {rules: {policy: {disallow-suppressions: null}}}", nil},
		{"off", "lint: {rules: {policy: {disallow-suppressions: off}}}", nil},
		{"on", "lint: {rules: {policy: {disallow-suppressions: on}}}", []string{"cache-call-unrestricted", "forbidden-suppression"}},
		{"mapping", "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {}}}}}", []string{"cache-call-unrestricted", "forbidden-suppression"}},
		{"all", "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: all}}}}}", []string{"cache-call-unrestricted", "forbidden-suppression"}},
		{"suppression", "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: suppression}}}}}", []string{"forbidden-suppression"}},
		{"violation", "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {report: violation}}}}}", []string{"cache-call-unrestricted"}},
		{"selected", "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {rules: [cache-call-unrestricted]}}}}}", []string{"cache-call-unrestricted", "forbidden-suppression"}},
		{"other rule", "lint: {rules: {policy: {disallow-suppressions: {level: on, options: {rules: [cache-operation]}}}}}", nil},
		{"disabled underlying", "lint: {rules: {security: {cache-call-unrestricted: off}, policy: {disallow-suppressions: on}}}", []string{"forbidden-suppression", "unused-suppression"}},
		{"disabled underlying violation only", "lint: {rules: {security: {cache-call-unrestricted: off}, policy: {disallow-suppressions: {level: on, options: {report: violation}}}}}", []string{"unused-suppression"}},
	} {
		for _, preceding := range []bool{false, true} {
			for _, discovery := range []bool{false, true} {
				name := tc.name + map[bool]string{false: "/trailing", true: "/preceding"}[preceding] + map[bool]string{false: "/explicit", true: "/discovery"}[discovery]
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					configPath := filepath.Join(dir, "config.yaml")
					if discovery {
						if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0755); err != nil {
							t.Fatal(err)
						}
						configPath = filepath.Join(dir, ".github", "actionlint.yaml")
					}
					if err := os.WriteFile(configPath, []byte(tc.config), 0600); err != nil {
						t.Fatal(err)
					}
					job := "  report: { uses: example/repository/.github/workflows/report.yml@main }"
					body := job + " # actionlint:ignore cache-call-unrestricted -- reviewed callee\n"
					if preceding {
						body = "  # actionlint:ignore-next-line cache-call-unrestricted -- reviewed callee\n" + job + "\n"
					}
					var stdout, stderr strings.Builder
					source := `on: pull_request_target
jobs:
` + body + "\npermissions: {}\n"
					cmd := cli.Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
					args := []string{"actionlint", "--summary=false", "-no-tool=shellcheck", "-no-tool=ruff", "-no-color", "-format", "{{json .}}"}
					input := "-"
					if !discovery {
						args = append(args, "-config-file", configPath)
					} else {
						input = filepath.Join(dir, ".github", "workflows", "test.yaml")
						if err := os.WriteFile(input, []byte(source), 0600); err != nil {
							t.Fatal(err)
						}
					}
					status := cmd.Main(append(args, input))
					var diagnostics []struct {
						Kind string
						Line int
					}
					if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
						t.Fatal(err)
					}
					var kinds []string
					for _, d := range diagnostics {
						kinds = append(kinds, d.Kind)
						wantLine := 3
						if preceding && d.Kind == "cache-call-unrestricted" {
							wantLine = 4
						}
						if d.Line != wantLine {
							t.Errorf("%s line = %d, want %d", d.Kind, d.Line, wantLine)
						}
					}
					// Source order differs between trailing and preceding directives.
					if diff := cmp.Diff(kindCounts(tc.kinds), kindCounts(kinds)); diff != "" {
						t.Fatal(diff)
					}
					wantStatus := commandstatus.ExitSuccessNoProblem
					if len(tc.kinds) != 0 {
						wantStatus = commandstatus.ExitSuccessProblemFound
					}
					if status != wantStatus || stderr.Len() != 0 {
						t.Fatalf("status=%d, want=%d; stderr=%s", status, wantStatus, &stderr)
					}
				})
			}
		}
	}
}

func TestDisallowSuppressionsOutputAndFilters(t *testing.T) {
	t.Parallel()
	const source = `on: pull_request_target
cache-mode: write # actionlint:ignore cache-write-untrusted -- reviewed
jobs:
  test:
` + cachePolicySteps + "\npermissions: {}\n"
	const forbidden = "disallowed by policy"
	const violation = "poison caches"
	for _, tc := range []struct {
		name, cliIgnore, configIgnore, format string
		want                                  []string
	}{
		{"SARIF", "", "", "sarif", []string{"cache-write-untrusted", "forbidden-suppression"}},
		{"CLI removes directive", forbidden, "", "{{json .}}", []string{"cache-write-untrusted"}},
		{"CLI removes violation", violation, "", "{{json .}}", []string{"forbidden-suppression"}},
		{"path removes directive", "", "  message-ignores: {'test.yaml': ['" + forbidden + "']}\n", "{{json .}}", []string{"cache-write-untrusted"}},
		{"all paths remove violation", "", "  message-ignores: {'**': ['" + violation + "']}\n", "{{json .}}", []string{"forbidden-suppression"}},
		{"overlapping filters", forbidden, "  message-ignores: {'test.yaml': ['" + violation + "']}\n", "{{json .}}", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if err := os.WriteFile(config, []byte("lint:\n  rules: {policy: {disallow-suppressions: on}}\n"+tc.configIgnore), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr strings.Builder
			cmd := cli.Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			args := []string{"actionlint", "--summary=false", "-no-tool=shellcheck", "-no-tool=ruff", "-no-color", "-config-file", config, "-stdin-filename", "test.yaml"}
			if tc.format == "sarif" {
				args = append(args, "-output", "sarif")
			} else {
				args = append(args, "-format", tc.format)
			}
			if tc.cliIgnore != "" {
				args = append(args, "--ignore-regex", tc.cliIgnore)
			}
			status := cmd.Main(append(args, "-"))
			wantStatus := commandstatus.ExitSuccessNoProblem
			if len(tc.want) != 0 {
				wantStatus = commandstatus.ExitSuccessProblemFound
			}
			if status != wantStatus || stderr.Len() != 0 {
				t.Fatalf("status=%d, want=%d; stderr=%s", status, wantStatus, &stderr)
			}
			var kinds []string
			if tc.format == "sarif" {
				var report struct {
					Runs []struct {
						Tool struct {
							Driver struct{ Rules []struct{ ID string } }
						}
						Results []struct{ RuleID string }
					}
				}
				if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
					t.Fatal(err)
				}
				if len(report.Runs) != 1 {
					t.Fatalf("unexpected report: %s", &stdout)
				}
				descriptors := map[string]bool{}
				for _, rule := range report.Runs[0].Tool.Driver.Rules {
					descriptors[rule.ID] = true
				}
				for _, result := range report.Runs[0].Results {
					kinds = append(kinds, result.RuleID)
					if !descriptors[result.RuleID] {
						t.Errorf("missing descriptor: %s", result.RuleID)
					}
				}
			} else {
				var diagnostics []struct{ Kind string }
				if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
					t.Fatal(err)
				}
				for _, diagnostic := range diagnostics {
					kinds = append(kinds, diagnostic.Kind)
				}
			}
			if diff := cmp.Diff(kindCounts(tc.want), kindCounts(kinds)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func kindCounts(kinds []string) map[string]int {
	counts := map[string]int{}
	for _, kind := range kinds {
		counts[kind]++
	}
	return counts
}
