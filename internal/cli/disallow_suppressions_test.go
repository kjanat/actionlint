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

func TestDisallowSuppressionsCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name, config string
		kinds        []string
	}{
		{"absent", "", nil}, {"empty policy", "policy: {}", nil}, {"null policy", "policy: null", nil},
		{"null", "policy: {disallow-suppressions: null}", nil},
		{"false", "policy: {disallow-suppressions: false}", nil},
		{"true", "policy: {disallow-suppressions: true}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"mapping", "policy: {disallow-suppressions: {}}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"all", "policy: {disallow-suppressions: {report: all}}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"suppression", "policy: {disallow-suppressions: {report: suppression}}", []string{"disallow-suppressions"}},
		{"violation", "policy: {disallow-suppressions: {report: violation}}", []string{"cache-call-unrestricted"}},
		{"selected", "policy: {disallow-suppressions: {rules: [cache-call-unrestricted]}}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"other rule", "policy: {disallow-suppressions: {rules: [cache-operation]}}", nil},
		{"disabled underlying", "policy: {cache-call-unrestricted: false, disallow-suppressions: true}", []string{"disallow-suppressions"}},
		{"disabled underlying violation only", "policy: {cache-call-unrestricted: false, disallow-suppressions: {report: violation}}", nil},
	} {
		for _, preceding := range []bool{false, true} {
			for _, discovery := range []bool{false, true} {
				name := tc.name + map[bool]string{false: "/trailing", true: "/preceding"}[preceding] + map[bool]string{false: "/explicit", true: "/discovery"}[discovery]
				t.Run(name, func(t *testing.T) {
					t.Chdir(t.TempDir())
					configPath := "config.yaml"
					if discovery {
						if err := os.Mkdir(".git", 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.MkdirAll(filepath.Join(".github", "workflows"), 0755); err != nil {
							t.Fatal(err)
						}
						configPath = filepath.Join(".github", "actionlint.yaml")
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
					source := "on: pull_request_target\njobs:\n" + body
					cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
					args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", "{{json .}}"}
					input := "-"
					if !discovery {
						args = append(args, "-config-file", configPath)
					} else {
						input = filepath.Join(".github", "workflows", "test.yaml")
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
					wantStatus := actionlint.ExitStatusSuccessNoProblem
					if len(tc.kinds) != 0 {
						wantStatus = actionlint.ExitStatusSuccessProblemFound
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
	t.Chdir(t.TempDir())
	const source = "on: pull_request_target\ncache-mode: write # actionlint:ignore cache-write-untrusted -- reviewed\njobs:\n  test:\n" + cachePolicySteps
	const forbidden = "disallowed by policy"
	const violation = "poison caches"
	for _, tc := range []struct {
		name, cliIgnore, configIgnore, format string
		want                                  []string
	}{
		{"SARIF", "", "", actionlint.SARIFTemplate(), []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"CLI removes directive", forbidden, "", "{{json .}}", []string{"cache-write-untrusted"}},
		{"CLI removes violation", violation, "", "{{json .}}", []string{"disallow-suppressions"}},
		{"path removes directive", "", "paths: {'test.yaml': {ignore: ['" + forbidden + "']}}", "{{json .}}", []string{"cache-write-untrusted"}},
		{"all paths remove violation", "", "paths: {'**': {ignore: ['" + violation + "']}}", "{{json .}}", []string{"disallow-suppressions"}},
		{"overlapping filters", forbidden, "paths: {'test.yaml': {ignore: ['" + violation + "']}}", "{{json .}}", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if err := os.WriteFile(config, []byte("policy: {disallow-suppressions: true}\n"+tc.configIgnore), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr strings.Builder
			cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", tc.format, "-config-file", config, "-stdin-filename", "test.yaml"}
			if tc.cliIgnore != "" {
				args = append(args, "-ignore", tc.cliIgnore)
			}
			status := cmd.Main(append(args, "-"))
			wantStatus := actionlint.ExitStatusSuccessNoProblem
			if len(tc.want) != 0 {
				wantStatus = actionlint.ExitStatusSuccessProblemFound
			}
			if status != wantStatus || stderr.Len() != 0 {
				t.Fatalf("status=%d, want=%d; stderr=%s", status, wantStatus, &stderr)
			}
			var kinds []string
			if tc.format == actionlint.SARIFTemplate() {
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
